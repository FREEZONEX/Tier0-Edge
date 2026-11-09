package sourcestream

import (
	"backend/internal/repo"
	pb "backend/internal/salesforcepubsub"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"errors"

	"fmt"
	"github.com/google/uuid"
	"github.com/linkedin/goavro/v2"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

const salesforceJournalLimit = 512

var salesforceWorkers sync.Map
var errSalesforceReplayGap = errors.New("SourceStream.salesforceReplayGap")

func salesforceOwner(j *durableJournal, leaseID string) bool {
	var owner struct {
		Owner string `json:"owner"`
	}
	return json.Unmarshal(j.Metadata, &owner) == nil && owner.Owner == leaseID
}

type salesforceWorker struct {
	service  *Service
	sourceID int64
	cancel   context.CancelFunc
}

func (s *Service) closeSalesforceSource(id int64) {
	salesforceWorkers.Range(func(_, value any) bool {
		w := value.(*salesforceWorker)
		if w.service == s && w.sourceID == id {
			w.cancel()
		}
		return true
	})
}
func (s *Service) closeSalesforce() {
	salesforceWorkers.Range(func(_, value any) bool {
		w := value.(*salesforceWorker)
		if w.service == s {
			w.cancel()
		}
		return true
	})
}

func (s *Service) exchangeSalesforceCDC(ctx context.Context, req EdgeSubscriptionRequest, key string, row repo.Source, def SourceDefinition) (EdgeSubscriptionBatch, error) {
	if def.ConnectionRole() != ClientRole || def.Protocol != OAuth || def.SourceType != "salesforce" {
		return EdgeSubscriptionBatch{}, ErrForbidden
	}
	if req.Action == "cancel" {
		return s.exchangeDurableJournal(ctx, req, key, row, salesforceJournalLimit)
	}
	if req.Action != "exchange" || req.ConfigRevision != row.ConfigRevision || row.DesiredState != "enabled" {
		return EdgeSubscriptionBatch{}, repo.ErrConflict
	}
	if len(req.Resources) != 1 || req.QoS != 0 || req.SamplingIntervalMs != 0 || req.PollIntervalMs != 0 || len(req.Ranges) != 0 {
		return EdgeSubscriptionBatch{}, repo.ErrInvalidArgument
	}
	for _, ref := range req.Resources {
		_, mode, err := salesforceSelection(def, ref)
		if err != nil || mode != "cdc" {
			return EdgeSubscriptionBatch{}, invalid("resources", "undeclared")
		}
	}
	out, err := s.exchangeDurableJournal(ctx, req, key, row, salesforceJournalLimit)
	if err != nil || req.Action == "cancel" {
		return out, err
	}
	for _, ref := range req.Resources {
		workerKey := fmt.Sprintf("%p/%d/%d/%s", s, row.ID, row.ConfigRevision, ref)
		workerCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		worker := &salesforceWorker{service: s, sourceID: row.ID, cancel: cancel}
		if _, loaded := salesforceWorkers.LoadOrStore(workerKey, worker); !loaded {
			go func(ref, key string) {
				defer cancel()
				defer salesforceWorkers.Delete(key)
				s.salesforceCDCLoop(workerCtx, row, def, ref)
			}(ref, workerKey)
		} else {
			cancel()
		}
	}
	return out, nil
}

func (s *Service) salesforceCDCLoop(ctx context.Context, row repo.Source, def SourceDefinition, ref string) {
	db, err := repo.GetUnsConn(ctx).DB()
	if err != nil {
		return
	}
	lease, err := db.Conn(ctx)
	if err != nil {
		return
	}
	defer lease.Close()
	lockKey := fmt.Sprintf("salesforce-cdc:%d", row.ID)
	var acquired bool
	if err = lease.QueryRowContext(ctx, "SELECT pg_try_advisory_lock(hashtextextended($1,0))", lockKey).Scan(&acquired); err != nil || !acquired {
		return
	}
	defer func() {
		release, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_, _ = lease.ExecContext(release, "SELECT pg_advisory_unlock(hashtextextended($1,0))", lockKey)
	}()
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	leaseID := uuid.NewString()
	if err = withDurableJournal(ctx, row.ID, row.ConfigRevision, salesforceJournalLimit, func(j *durableJournal) error {
		j.Metadata, _ = json.Marshal(map[string]string{"owner": leaseID})
		return nil
	}); err != nil {
		return
	}
	go func() {
		timer := time.NewTicker(10 * time.Second)
		defer timer.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-timer.C:
				ping, stop := context.WithTimeout(ctx, 3*time.Second)
				e := lease.PingContext(ping)
				stop()
				if e != nil {
					cancel()
					return
				}
			}
		}
	}()
	for {
		active := false
		gap := false
		err := withDurableJournal(ctx, row.ID, row.ConfigRevision, salesforceJournalLimit, func(j *durableJournal) error {
			if !salesforceOwner(j, leaseID) {
				return repo.ErrConflict
			}
			active = j.active()
			gap = len(j.Gaps) > 0
			return nil
		})
		if err != nil || !active || gap {
			return
		}
		err = s.salesforceCDCSession(ctx, row, def, ref, leaseID)
		if err == nil {
			return
		}
		reason := "salesforceCDCUnavailable"
		if errors.Is(err, errSalesforceReplayGap) {
			reason = "salesforceReplayGap"
		}
		if e := withDurableJournal(ctx, row.ID, row.ConfigRevision, salesforceJournalLimit, func(j *durableJournal) error {
			if !salesforceOwner(j, leaseID) {
				return repo.ErrConflict
			}
			j.State = "error"
			j.Error = reason
			if reason == "salesforceReplayGap" {
				var last struct {
					ReplayID string `json:"replayId"`
				}
				_ = json.Unmarshal(j.Confirmed[ref].Data, &last)
				j.addGap(EdgeAcquisitionGap{Reason: reason, From: last.ReplayID, At: time.Now().UnixMilli()})
			}
			return nil
		}); e != nil {
			return
		}
		if reason == "salesforceReplayGap" {
			return
		}
		timer := time.NewTimer(2 * time.Second)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}

func (s *Service) salesforceCDCSession(parent context.Context, row repo.Source, def SourceDefinition, ref, leaseID string) error {
	q, _, err := salesforceSelection(def, ref)
	if err != nil {
		return err
	}
	endpoint := vendorParameter(def, "salesforcePubSubEndpoint")
	host, _, err := net.SplitHostPort(endpoint)
	origin, e := url.Parse(def.Endpoint)
	tenant := vendorParameter(def, "salesforceTenantId")
	if err != nil || e != nil || !strings.EqualFold(host, origin.Hostname()) || tenant == "" || strings.ContainsAny(tenant, "\r\n") {
		return ErrVendorResponse
	}
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	if def.HTTP.CACertificate != "" && !roots.AppendCertsFromPEM([]byte(def.HTTP.CACertificate)) {
		return ErrVendorResponse
	}
	token, err := s.oauthAccessToken(parent, row.ID, def)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(parent, 90*time.Second)
	defer cancel()
	// Reuse the Source-scoped DNS pinning and configured private-network policy.
	pubsubDefinition := def
	pubsubDefinition.Endpoint = "https://" + endpoint
	transportClient, e := s.httpManager.client(row, pubsubDefinition)
	if e != nil {
		return e
	}
	dial := transportClient.Transport.(*http.Transport).DialContext
	// The guarded dialer must receive the declared hostname, not an IP already
	// resolved by gRPC's default DNS resolver. It owns resolution and DNS pinning.
	conn, err := grpc.NewClient("passthrough:///"+endpoint, grpc.WithTransportCredentials(credentials.NewTLS(&tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: host})), grpc.WithContextDialer(func(ctx context.Context, address string) (net.Conn, error) { return dial(ctx, "tcp", address) }), grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(1024*1024)))
	if err != nil {
		return err
	}
	defer conn.Close()
	ctx = metadata.NewOutgoingContext(ctx, metadata.Pairs("accesstoken", token, "instanceurl", strings.TrimRight(def.Endpoint, "/"), "tenantid", tenant))
	client := pb.NewPubSubClient(conn)
	topic := "/data/" + q.Object + "ChangeEvent"
	info, err := client.GetTopic(ctx, &pb.TopicRequest{TopicName: topic})
	if err != nil {
		return s.salesforceCDCAuthError(parent, row, def, token, err)
	}
	if !info.CanSubscribe {
		return ErrForbidden
	}
	replay := ""
	err = withDurableJournal(ctx, row.ID, row.ConfigRevision, salesforceJournalLimit, func(j *durableJournal) error {
		if !salesforceOwner(j, leaseID) {
			return repo.ErrConflict
		}
		var state struct {
			ReplayID string `json:"replayId"`
		}
		_ = json.Unmarshal(j.Confirmed[ref].Data, &state)
		replay = state.ReplayID
		j.State = "listening"
		j.Error = ""
		return nil
	})
	if err != nil {
		return err
	}
	stream, err := client.Subscribe(ctx)
	if err != nil {
		return err
	}
	request := &pb.FetchRequest{TopicName: topic, ReplayPreset: pb.ReplayPreset_EARLIEST, NumRequested: 1}
	if replay != "" {
		request.ReplayPreset = pb.ReplayPreset_CUSTOM
		request.ReplayId, err = base64.StdEncoding.DecodeString(replay)
		if err != nil {
			return ErrVendorResponse
		}
	}
	if err = stream.Send(request); err != nil {
		return err
	}
	codecs := map[string]*goavro.Codec{}
	schemas := map[string][]string{}
	for {
		response, err := stream.Recv()
		if err != nil {
			if replay != "" && status.Code(err) == codes.InvalidArgument {
				return errSalesforceReplayGap
			}
			return s.salesforceCDCAuthError(parent, row, def, token, err)
		}
		if len(response.Events) > 1 || response.PendingNumRequested < 0 || response.PendingNumRequested > 1 {
			return ErrVendorResponse
		}
		for _, event := range response.Events {
			if event.Event == nil || len(event.ReplayId) == 0 || len(event.ReplayId) > 1024 {
				return ErrVendorResponse
			}
			schemaID := event.Event.SchemaId
			codec := codecs[schemaID]
			if codec == nil {
				if len(codecs) >= 64 {
					return ErrVendorResponse
				}
				schema, err := client.GetSchema(ctx, &pb.SchemaRequest{SchemaId: schemaID})
				if err != nil {
					return err
				}
				if schema.SchemaId != schemaID || len(schema.SchemaJson) > 1024*1024 {
					return ErrVendorResponse
				}
				codec, err = goavro.NewCodec(schema.SchemaJson)
				if err != nil {
					return ErrVendorResponse
				}
				codecs[schemaID] = codec
				var shape struct{ Fields []struct{ Name string } }
				if json.Unmarshal([]byte(schema.SchemaJson), &shape) != nil {
					return ErrVendorResponse
				}
				for _, f := range shape.Fields {
					schemas[schemaID] = append(schemas[schemaID], f.Name)
				}
			}
			decoded, rest, err := codec.NativeFromBinary(event.Event.Payload)
			if err != nil || len(rest) != 0 {
				return ErrVendorResponse
			}
			payload, ok := salesforceAvroValue(decoded).(map[string]any)
			if !ok {
				return ErrVendorResponse
			}
			header, ok := payload["ChangeEventHeader"].(map[string]any)
			if !ok {
				return ErrVendorResponse
			}
			change := vendorString(header["changeType"])
			if change != "CREATE" && change != "UPDATE" && change != "DELETE" && change != "UNDELETE" {
				return ErrVendorResponse
			}
			changed, err := salesforceBitmap(header["changedFields"], schemas[schemaID])
			if err != nil {
				return err
			}
			nulled, err := salesforceBitmap(header["nulledFields"], schemas[schemaID])
			if err != nil {
				return err
			}
			// Explicitly project declared fields; null on an unchanged UPDATE is not a clear operation.
			projected := map[string]any{"ChangeEventHeader": header}
			for _, f := range q.Fields {
				if value, exists := payload[f]; exists {
					projected[f] = value
				}
			}
			replayID := base64.StdEncoding.EncodeToString(event.ReplayId)
			raw, err := json.Marshal(map[string]any{"schemaId": schemaID, "replayId": replayID, "payload": projected, "changedFields": changed, "nulledFields": nulled})
			if err != nil {
				return err
			}
			err = withDurableJournal(ctx, row.ID, row.ConfigRevision, salesforceJournalLimit, func(j *durableJournal) error {
				var owner struct {
					Owner string `json:"owner"`
				}
				if json.Unmarshal(j.Metadata, &owner) != nil || owner.Owner != leaseID {
					return repo.ErrConflict
				}
				return j.append(ref, raw, time.Now().UnixMilli(), ref+":"+replayID)
			})
			if err != nil {
				return err
			}
		}
		active := false
		err = withDurableJournal(ctx, row.ID, row.ConfigRevision, salesforceJournalLimit, func(j *durableJournal) error {
			if !salesforceOwner(j, leaseID) {
				return repo.ErrConflict
			}
			active = j.active()
			return nil
		})
		if err != nil {
			return err
		}
		if !active {
			return nil
		}
		if response.PendingNumRequested == 0 {
			if err = stream.Send(&pb.FetchRequest{NumRequested: 1}); err != nil {
				return err
			}
		}
	}
}

func (s *Service) salesforceCDCAuthError(ctx context.Context, row repo.Source, def SourceDefinition, token string, err error) error {
	if status.Code(err) == codes.Unauthenticated {
		_ = s.rejectOAuthToken(ctx, row.ID, def, token)
	}
	return err
}

func salesforceAvroValue(value any) any {
	switch v := value.(type) {
	case map[string]any:
		if len(v) == 1 {
			for key, item := range v {
				if strings.Contains(key, ".") || key == "string" || key == "long" || key == "int" || key == "double" || key == "float" || key == "boolean" || key == "bytes" {
					return salesforceAvroValue(item)
				}
			}
		}
		out := map[string]any{}
		for k, item := range v {
			out[k] = salesforceAvroValue(item)
		}
		return out
	case []any:
		for i := range v {
			v[i] = salesforceAvroValue(v[i])
		}
		return v
	default:
		return value
	}
}

func salesforceBitmap(value any, fields []string) ([]string, error) {
	out := []string{}
	if value == nil {
		return out, nil
	}
	values, ok := value.([]any)
	if !ok {
		return nil, ErrVendorResponse
	}
	for _, item := range values {
		text, ok := item.(string)
		if !ok {
			return nil, ErrVendorResponse
		}
		if !strings.HasPrefix(text, "0x") {
			if !sfIdentifier.MatchString(text) {
				return nil, ErrVendorResponse
			}
			out = append(out, text)
			continue
		}
		bits, ok := new(big.Int).SetString(text[2:], 16)
		if !ok || bits.BitLen() > len(fields) {
			return nil, ErrVendorResponse
		}
		for i, f := range fields {
			if bits.Bit(i) != 0 {
				out = append(out, f)
			}
		}
	}
	return out, nil
}
