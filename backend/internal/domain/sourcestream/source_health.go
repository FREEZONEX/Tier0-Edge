package sourcestream

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"backend/internal/domain/sourcehealth"
	"backend/internal/infra/edgesql"
	"backend/internal/repo"
	"github.com/zeromicro/go-zero/core/logx"
)

// CheckSource 分开记录连接与数据验证；失败检查也是一个完整的诊断结果。
func (s *Service) CheckSource(ctx context.Context, id int64, mode string, timeoutMs int) (sourcehealth.Result, error) {
	if mode == "" {
		mode = sourcehealth.Connection
	}
	if mode != sourcehealth.Connection && mode != sourcehealth.Data {
		return sourcehealth.Result{}, invalid("mode", "connectionOrDataRequired")
	}
	if timeoutMs < 100 || timeoutMs > 10000 {
		return sourcehealth.Result{}, invalid("timeoutMs", "outOfRange")
	}
	started := time.Now()
	def, credential, revision, err := s.connection(ctx, id, nil)
	if err != nil {
		if errors.Is(err, ErrCredential) || errors.Is(err, ErrCredentialKey) || errors.Is(err, ErrOAuthRequired) {
			current, readErr := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
			if readErr != nil {
				return sourcehealth.Result{}, readErr
			}
			if current.DesiredState == "disabled" {
				return sourcehealth.Result{}, repo.ErrConflict
			}
			result := sourcehealth.New(mode, "authentication", started, sourcehealth.Fail("authentication", "SOURCE_AUTH_FAILED", "Source credentials are unavailable or were rejected", err))
			observed, finish := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
			defer finish()
			if saveErr := repo.NewSourceStreamRepo(observed).ObserveSourceCheck(observed, id, current.Revision, result); saveErr != nil {
				return sourcehealth.Result{}, saveErr
			}
			return result, nil
		}
		return sourcehealth.Result{}, err
	}
	bounded, cancel := context.WithTimeout(ctx, time.Duration(timeoutMs)*time.Millisecond)
	defer cancel()
	stage := "connect"
	if mode == sourcehealth.Connection {
		stage, err = s.checkConnection(bounded, id, def, credential)
	} else {
		stage = "validation"
		if def.Protocol == FileRead {
			stage = "download"
		}
		if potentiallyWritingSQL(def) {
			err = sourcehealth.Fail("validation", "SOURCE_DATA_CHECK_UNSUPPORTED", "Automatic validation cannot execute potentially writing SQL queries", nil)
		} else if def.Protocol == MQTT || def.Protocol == USBHID || def.ConnectionRole() == ServerRole {
			err = sourcehealth.Fail("validation", "SOURCE_DATA_CHECK_UNSUPPORTED", "Use a bounded Stream sample test to validate event or listener data", nil)
		} else if def.Protocol == ModbusTCP {
			if def.Modbus == nil || len(def.Modbus.Ranges) == 0 {
				err = sourcehealth.Fail("validation", "SOURCE_DATA_CHECK_UNSUPPORTED", "Configure a readable Modbus range or use a bounded Stream sample test", nil)
			} else {
				row, e := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
				err = e
				if err == nil {
					for _, selected := range def.Modbus.Ranges {
						_, err = s.ReadReference(bounded, id, row.ConfigRevision, selected, timeoutMs)
						if err != nil {
							break
						}
					}
				}
			}
		} else if def.Protocol == OPCUA {
			if def.OPCUA == nil || len(def.OPCUA.NodeIDs) == 0 {
				err = sourcehealth.Fail("validation", "SOURCE_DATA_CHECK_UNSUPPORTED", "Configure readable OPC UA nodes or use a bounded Stream sample test", nil)
			} else {
				row, e := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
				err = e
				limit := 1
				if err == nil {
					for _, node := range def.OPCUA.NodeIDs {
						_, err = s.EdgeResourceRead(bounded, id, row.ConfigRevision, "opcua:"+node, EdgeResourceReadParams{Limit: &limit}, timeoutMs)
						if err != nil {
							break
						}
					}
				}
			}
		} else if def.Protocol == FileRead || def.Protocol == EdgeSQL {
			row, e := repo.NewSourceStreamRepo(ctx).Source(ctx, id)
			err = e
			if err == nil {
				for _, resource := range declaredResources(def, row) {
					_, err = s.extendedPage(bounded, row, def, resource.ResourceRef, "", 1, 0, timeoutMs, true)
					if err != nil {
						break
					}
				}
			}
		} else {
			_, err = s.probeData(bounded, id, nil, timeoutMs)
		}
	}
	if err != nil && bounded.Err() != nil {
		var failure *sourcehealth.Failure
		if !errors.As(err, &failure) {
			err = bounded.Err()
		}
	}
	if errors.Is(err, ErrCredential) || errors.Is(err, ErrCredentialKey) || errors.Is(err, ErrOAuthRequired) {
		err = sourcehealth.Fail("authentication", "SOURCE_AUTH_FAILED", "Source credentials are unavailable or were rejected", err)
	}
	var validation *ValidationError
	if errors.As(err, &validation) {
		code, message := "SOURCE_DATA_INVALID", "Source data validation failed: "+sourcehealth.SafeCode(validation.Code)
		switch validation.Code {
		case "standaloneSQLiteRequired":
			code, message = "SOURCE_FILE_FORMAT_INVALID", "Expected a standalone SQLite snapshot"
		case "corruptSnapshot":
			code, message = "SOURCE_SNAPSHOT_CORRUPT", "SQLite snapshot integrity validation failed"
		case "ordinaryTableRequired":
			code, message = "SOURCE_TABLE_MISSING", "Configured SQLite table is missing or is not an ordinary table"
		case "ambiguousBusinessKey":
			code, message = "SOURCE_KEY_INVALID", "Configured table business keys are empty, invalid or duplicated"
		case "tooLarge":
			code, message = "SOURCE_FILE_TOO_LARGE", "Snapshot exceeds the allowed file size"
		case "sizeMismatch":
			code, message = "SOURCE_FILE_INCOMPLETE", "Snapshot download size does not match the declared size"
		case "unsupportedCapability":
			code, message = "SOURCE_DATA_CHECK_UNSUPPORTED", "This configured resource does not support a bounded read-only data check"
		}
		err = sourcehealth.Fail("validation", code, message, err)
	}
	if edgesql.DiagnosticCode(err) != "" {
		if diagnostic := SourceReadDiagnostic(err); diagnostic != nil {
			healthStage := diagnostic.Stage
			switch diagnostic.Code {
			case "SOURCE_DNS_FAILED":
				healthStage = "dns"
			case "SOURCE_TLS_FAILED":
				healthStage = "tls"
			}
			err = sourcehealth.Fail(healthStage, diagnostic.Code, diagnostic.Message, err)
		}
	}
	result := sourcehealth.New(mode, stage, started, err)
	observed, finish := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
	defer finish()
	if err := repo.NewSourceStreamRepo(observed).ObserveSourceCheck(observed, id, revision, result); err != nil {
		return sourcehealth.Result{}, err
	}
	return result, nil
}

func (s *Service) checkConnection(ctx context.Context, id int64, def SourceDefinition, c Credential) (string, error) {
	switch {
	case def.Protocol == FileRead:
		u, _ := url.Parse(def.Endpoint)
		if u.Scheme == "http" || u.Scheme == "https" {
			selection := FileSelection{}
			if len(def.File.Resources) > 0 {
				selection = def.File.Resources[0]
			} else {
				selection.Path = def.File.Directories[0].Path
			}
			response, err := fileHTTPGet(ctx, def, selection, c, http.Header{"Range": []string{"bytes=0-0"}})
			if err != nil {
				return "connect", err
			}
			defer response.Body.Close()
			if response.StatusCode == 401 || response.StatusCode == 403 {
				return "authentication", ErrCredential
			}
			if response.StatusCode != 200 && response.StatusCode != 206 {
				return "http", sourcehealth.Fail("http", "SOURCE_HTTP_STATUS", "Source file endpoint returned HTTP "+response.Status, nil)
			}
			_, err = io.Copy(io.Discard, io.LimitReader(response.Body, 1))
			return "response", err
		}
		// 非 HTTP 文件传输只检查网络可达性；完整认证和文件校验属于 data。
		port := u.Port()
		if port == "" {
			port = "445"
			if u.Scheme == "sftp" {
				port = "22"
			}
		}
		conn, err := dialFileSource(ctx, "tcp", net.JoinHostPort(u.Hostname(), port))
		if err == nil {
			err = conn.Close()
		}
		return "connect", err
	case def.Protocol == EdgeSQL:
		if def.SQL == nil {
			return "configuration", ErrUnavailable
		}
		if len(def.SQL.Queries) > 0 {
			return "authentication", probeNamedSQL(ctx, def, c)
		}
		endpoint, err := url.Parse(def.Endpoint)
		if err != nil {
			return "configuration", err
		}
		trust := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Hostname()}
		if def.tlsConfig != nil {
			trust = def.tlsConfig.Clone()
		}
		if def.SQL.CACertificate != "" {
			if trust.RootCAs == nil {
				trust.RootCAs = x509.NewCertPool()
			}
			if !trust.RootCAs.AppendCertsFromPEM([]byte(def.SQL.CACertificate)) {
				return "tls", ErrCredential
			}
		}
		db, err := edgesql.Open(endpoint, c.Username, c.Secret, trust)
		if err != nil {
			return "connect", err
		}
		defer db.Close()
		return "authentication", edgesql.SafeError(ctx, db.PingContext(ctx))
	case (def.Protocol == HTTP || def.Protocol == OAuth) && def.ConnectionRole() != ServerRole:
		// 仅连接和 TLS，不调用可能昂贵的业务 URL 或 OAuth token 端点。
		endpoint, err := url.Parse(def.Endpoint)
		if err != nil {
			return "configuration", err
		}
		port := endpoint.Port()
		if port == "" {
			port = "80"
			if endpoint.Scheme == "https" {
				port = "443"
			}
		}
		conn, err := dialFileSource(ctx, "tcp", net.JoinHostPort(endpoint.Hostname(), port))
		if err != nil {
			return "connect", err
		}
		defer conn.Close()
		if endpoint.Scheme == "https" {
			trust := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: endpoint.Hostname()}
			if def.tlsConfig != nil {
				trust = def.tlsConfig.Clone()
				trust.ServerName = endpoint.Hostname()
			}
			if def.HTTP != nil && def.HTTP.CACertificate != "" {
				if trust.RootCAs == nil {
					trust.RootCAs = x509.NewCertPool()
				}
				trust.RootCAs.AppendCertsFromPEM([]byte(def.HTTP.CACertificate))
			}
			return "tls", tls.Client(conn, trust).HandshakeContext(ctx)
		}
		return "connect", nil
	case def.Protocol == HTTP && def.ConnectionRole() == ServerRole:
		return "listener", s.probeHTTPServer(id, def, c)
	case def.Protocol == OpenProtocol:
		endpoint, err := url.Parse(def.Endpoint)
		if err != nil {
			return "configuration", err
		}
		conn, err := dialFileSource(ctx, "tcp", endpoint.Host)
		if err == nil {
			err = conn.Close()
		}
		return "connect", err
	default:
		if s.runtime == nil {
			return "runtime", ErrUnavailable
		}
		return "protocol", s.runtime.Probe(ctx, id, def.Connection(), c)
	}
}

// 每分钟只调度已到 10 分钟周期的 Source；持久租约防止多实例和重启风暴。
func (s *Service) runSourceHealth(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			for i := 0; i < 16 && ctx.Err() == nil; i++ {
				id, release, err := repo.NewSourceStreamRepo(ctx).ClaimSourceHealth(ctx, time.Now(), sourcehealth.Interval)
				if err != nil {
					logx.WithContext(ctx).Error("Source health scheduler claim failed")
					break
				}
				if id == 0 {
					break
				}
				if _, err := s.CheckSource(ctx, id, sourcehealth.Connection, 10000); err != nil {
					logx.WithContext(ctx).Error("Source health check observation failed")
				}
				release()
			}
		}
	}
}

// 原生旧入口也使用轻量检查，保留其错误返回约定。
func (s *Service) Probe(ctx context.Context, id int64, input any, timeoutMs int) (map[string]any, error) {
	if input != nil {
		return nil, invalid("document", "saveBeforeTest")
	}
	result, err := s.CheckSource(ctx, id, sourcehealth.Connection, timeoutMs)
	if err != nil {
		return nil, err
	}
	if result.Health != "healthy" {
		cause := error(nil)
		if result.ErrorCode == "SOURCE_TIMEOUT" {
			cause = context.DeadlineExceeded
		}
		return nil, sourcehealth.Fail(result.Stage, result.ErrorCode, result.Message, cause)
	}
	return map[string]any{"health": result.Health, "checkedAt": result.CheckedAt, "mode": result.Mode, "message": result.Message}, nil
}
