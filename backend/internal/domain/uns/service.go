package uns

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	domaincommon "backend/internal/domain/common"
	"backend/internal/domain/dataingest"
	domaindataquality "backend/internal/domain/dataquality"
	"backend/internal/events"
	"backend/internal/infra/outbox"
	"backend/internal/repo"

	"github.com/google/uuid"
	"github.com/mozillazg/go-pinyin"
	"gorm.io/gorm"
)

type Service struct {
	uns                 *repo.UnsRepo
	jobs                *repo.AsyncJobRepo
	bus                 *events.Bus
	outbox              *outbox.Service
	payload             *dataingest.Service
	legacyFlowsDisabled bool
}

type SaveCommand struct {
	ID                int64
	ParentID          int64
	Name              string
	DisplayName       string
	Description       string
	Alias             string
	Namespace         string
	NodeType          string
	TopicType         string
	Schema            string
	ExtendProperties  string
	DataQualityPolicy *repo.DataQualityPolicy
	LabelIDs          []int64
	AssetFileIDs      []int64
	EnableHistory     *bool
	AddFlow           bool
	MockData          bool
	// MockIntervalSeconds 自定义 mock 数据发送间隔（秒），0 表示默认（10s）
	MockIntervalSeconds int
	// MockIntervalMillis 亚秒级间隔（毫秒），优先于 MockIntervalSeconds
	MockIntervalMillis int
	// MockFunctionJS 自定义 mock function 节点代码（构造 msg.payload 并 return msg）
	MockFunctionJS    string
	UserID            int64
	PreserveAlias     bool
	SystemTopicFolder bool
}

type DashboardQuery struct {
	NodeID    int64
	TimeStart int64
	TimeEnd   int64
	Limit     int
}

type DeletedEvent struct {
	Nodes []repo.UnsNode `json:"nodes"`
}

type RestoredEvent struct {
	Nodes []repo.UnsNode `json:"nodes"`
}

type TemplateCommand struct {
	ID          int64
	Name        string
	Description string
	TopicType   string
	Fields      []map[string]interface{}
	UserID      int64
}

type LabelCommand struct {
	ID          int64
	Name        string
	Color       string
	Description string
	UserID      int64
}

type ExportCommand struct {
	ExportType string
	Folders    []int64
	Files      []int64
}

type ImportStatus struct {
	Module   string  `json:"module,omitempty"`
	Code     int     `json:"code"`
	Msg      string  `json:"msg,omitempty"`
	Task     string  `json:"task,omitempty"`
	Finished bool    `json:"finished,omitempty"`
	Progress float64 `json:"progress,omitempty"`
	Imported int     `json:"imported,omitempty"`
	Skipped  int     `json:"skipped,omitempty"`
	Errors   int     `json:"errors,omitempty"`
}

type importFileData struct {
	Type              string                  `json:"type"`
	Name              string                  `json:"name"`
	Alias             string                  `json:"alias"`
	DisplayName       string                  `json:"displayName"`
	Fields            []map[string]any        `json:"fields"`
	LegacySchema      []map[string]any        `json:"schema"`
	DataType          any                     `json:"dataType"`
	Description       string                  `json:"description"`
	Label             string                  `json:"label"`
	Labels            any                     `json:"labels"`
	GenerateDashboard string                  `json:"generateDashboard"`
	EnableHistory     string                  `json:"enableHistory"`
	MockData          string                  `json:"mockData"`
	WriteData         string                  `json:"writeData"`
	TopicType         string                  `json:"topicType"`
	ParentDataType    string                  `json:"parentDataType"`
	Children          []importFileData        `json:"children"`
	ExtendProperties  map[string]any          `json:"extendProperties"`
	Extend            map[string]any          `json:"extend"`
	DataQualityPolicy *repo.DataQualityPolicy `json:"dataQualityPolicy"`
}

const (
	topicTypeMetricValue = int16(3)
)

const (
	aliasPrefixMaxRunes  = 12
	aliasSuffixHexLength = 12
)

var reservedCreateFolderNames = map[string]struct{}{
	"label": {},
}

func New(ctx context.Context, payloadSvc *dataingest.Service) *Service {
	return &Service{
		uns:     repo.NewUnsRepo(ctx),
		jobs:    repo.NewAsyncJobRepo(ctx),
		bus:     events.Default(),
		outbox:  outbox.New(),
		payload: payloadSvc,
	}
}

// DisableLegacyFlows keeps existing metadata intact while preventing new
// Node-RED tasks in deployments using Source/Stream.
func (s *Service) DisableLegacyFlows() { s.legacyFlowsDisabled = true }

func (s *Service) publishPostCommit(ctx context.Context, event any) {
	if s == nil || s.bus == nil {
		return
	}
	_ = s.bus.Publish(ctx, events.PhaseAfterCommit, event)
	_ = s.bus.Publish(ctx, events.PhaseAsync, event)
}

func (s *Service) publishInTx(ctx context.Context, event any) error {
	if s == nil || s.bus == nil {
		return nil
	}
	return s.bus.Publish(ctx, events.PhaseInTx, event)
}

func (s *Service) List(ctx context.Context, parentID int64, parentIDSet bool, keyword string, includeRecycle bool) (map[string]any, error) {
	nodes, err := s.uns.ListUnsNodes(ctx, repo.UnsNodeFilter{ParentID: parentID, ParentIDSet: parentIDSet, Keyword: keyword, IncludeRecycle: includeRecycle})
	if err != nil {
		return nil, err
	}
	return listResp(nodes), nil
}

func (s *Service) ActiveNodeCount(ctx context.Context) (int64, error) {
	return s.uns.CountActiveUnsNodes(ctx)
}

func (s *Service) Recycle(ctx context.Context) (map[string]any, error) {
	nodes, err := s.uns.ListUnsNodes(ctx, repo.UnsNodeFilter{RecycleOnly: true})
	if err != nil {
		return nil, err
	}
	return listResp(nodes), nil
}

func (s *Service) Detail(ctx context.Context, id int64, includeDeleted bool) (map[string]any, error) {
	node, err := s.uns.GetUnsNode(ctx, id, includeDeleted)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	resp := nodeResp(node)
	if node.Type == 2 {
		if s.payload != nil {
			if payload, err := s.payload.LastPayload(ctx, node); err == nil && len(payload) > 0 {
				resp["lastPayload"] = payload
			}
		}
	}
	return resp, nil
}

func (s *Service) Dashboard(ctx context.Context, query DashboardQuery) (map[string]any, error) {
	node, err := s.uns.GetUnsNode(ctx, query.NodeID, false)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	if node.Type != 2 {
		return nil, ErrInvalid
	}
	if s.payload == nil {
		return map[string]any{
			"fields": []map[string]any{},
			"list":   []map[string]any{},
			"total":  0,
		}, nil
	}
	return s.payload.History(ctx, node, query.TimeStart, query.TimeEnd, query.Limit)
}

func (s *Service) Create(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	if resp, handled, err := s.createByNamespacePath(ctx, cmd); handled || err != nil {
		return resp, err
	}
	return s.createSingle(ctx, cmd)
}

func (s *Service) NormalizeOpenapiCreateCommand(ctx context.Context, cmd SaveCommand) (SaveCommand, error) {
	fullPath := strings.Trim(cmd.Namespace, "/")
	if fullPath == "" {
		fullPath = strings.Trim(cmd.Name, "/")
	}
	parts := splitNamespaceParts(fullPath)
	name := strings.TrimSpace(cmd.Name)
	parentTopicType := int16(0)
	if len(parts) > 0 {
		name = parts[len(parts)-1]
		if len(parts) >= 2 {
			parentTopicType = systemTopicTypeByName(parts[len(parts)-2])
		}
	} else if cmd.ParentID != 0 {
		parent, err := s.uns.GetUnsNode(ctx, cmd.ParentID, false)
		if err != nil {
			return cmd, err
		}
		if parent.Type != 1 {
			return cmd, ErrInvalid
		}
		parentTopicType = systemTopicTypeByName(parent.Name)
	}

	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil {
		return cmd, err
	}
	if strings.TrimSpace(cmd.NodeType) == "" {
		if parentTopicType != 0 {
			nodeType = 2
		} else {
			nodeType = 1
		}
		cmd.NodeType = nodeTypeName(nodeType)
	}

	topicType, err := topicTypeValue(cmd.TopicType)
	if err != nil {
		return cmd, err
	}
	nameTopicType := systemTopicTypeByName(name)
	switch nodeType {
	case 1:
		if parentTopicType != 0 {
			return cmd, ErrInvalid
		}
		if nameTopicType != 0 {
			if topicType != 0 && topicType != nameTopicType {
				return cmd, ErrInvalid
			}
			cmd.TopicType = topicTypeName(nameTopicType)
			cmd.SystemTopicFolder = true
		} else if topicType != 0 {
			return cmd, ErrInvalid
		} else {
			cmd.TopicType = ""
		}
	case 2:
		if parentTopicType != 0 {
			if topicType != 0 && topicType != parentTopicType {
				return cmd, ErrInvalid
			}
			cmd.TopicType = topicTypeName(parentTopicType)
		} else if topicType == 0 {
			return cmd, ErrInvalid
		}
	default:
		return cmd, ErrInvalid
	}
	return cmd, nil
}

func (s *Service) createSingle(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	if cmd.AddFlow || cmd.MockData {
		return nil, ErrInvalid
	}
	if cmd.AddFlow {
		cmd.MockData = true
	}
	cmd = s.inferTopicTypeFromParent(ctx, cmd)
	var err error
	cmd, err = s.ensureTopicTypeParentForFile(ctx, cmd)
	if err != nil {
		return nil, err
	}
	node, err := s.commandToNode(ctx, cmd)
	if err != nil {
		return nil, err
	}
	if err := s.publishInTx(ctx, events.UnsNodeSaving{Action: "create", Node: node, UserID: cmd.UserID}); err != nil {
		return nil, err
	}
	if err := s.validateParentRule(ctx, node); err != nil {
		return nil, err
	}
	node.CreatedBy = cmd.UserID
	var created repo.UnsNode
	err = repo.GetCommonConn(ctx).Transaction(func(tx *gorm.DB) error {
		var createErr error
		created, createErr = repo.NewUnsRepo(tx).CreateUnsNode(ctx, node, cmd.LabelIDs, cmd.AssetFileIDs)
		if createErr != nil {
			return createErr
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	s.publishPostCommit(ctx, events.UnsNodeCreated{Node: created})
	resp := nodeResp(created)
	if created.Type == 2 && cmd.AddFlow {
		resp["withFlow"] = true
		resp["mockData"] = cmd.MockData
	}
	return resp, nil
}

func (s *Service) inferTopicTypeFromParent(ctx context.Context, cmd SaveCommand) SaveCommand {
	if strings.TrimSpace(cmd.TopicType) != "" || cmd.ParentID == 0 {
		return cmd
	}
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil || nodeType != 2 {
		return cmd
	}
	parent, err := s.uns.GetUnsNode(ctx, cmd.ParentID, false)
	if err != nil || parent.TopicType == 0 {
		return cmd
	}
	cmd.TopicType = topicTypeName(parent.TopicType)
	return cmd
}

func (s *Service) ensureTopicTypeParentForFile(ctx context.Context, cmd SaveCommand) (SaveCommand, error) {
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil {
		return cmd, err
	}
	if nodeType != 2 {
		return cmd, nil
	}
	topicType, err := topicTypeValue(cmd.TopicType)
	if err != nil {
		return cmd, err
	}
	if topicType == 0 {
		if namespaceTopicType := topicTypeFromNamespace(cmd.Namespace); namespaceTopicType != 0 {
			topicType = namespaceTopicType
		} else {
			topicType = 1
		}
		cmd.TopicType = topicTypeName(topicType)
	}
	if cmd.ParentID != 0 {
		parent, err := s.uns.GetUnsNode(ctx, cmd.ParentID, false)
		if err != nil {
			return cmd, err
		}
		if parent.Type != 1 {
			return cmd, ErrInvalid
		}
		if parentNameTopicType := systemTopicTypeByName(parent.Name); parentNameTopicType != 0 {
			if parentNameTopicType == topicType {
				return cmd, nil
			}
			return cmd, ErrInvalid
		}
		if parent.TopicType == topicType {
			return cmd, nil
		}
		if parent.TopicType != 0 {
			return cmd, ErrInvalid
		}
	}
	folder, err := s.ensureTopicTypeFolder(ctx, cmd.ParentID, topicType, cmd.UserID)
	if err != nil {
		return cmd, err
	}
	cmd.ParentID = folder.ID
	return cmd, nil
}

func (s *Service) ensureTopicTypeFolder(ctx context.Context, parentID int64, topicType int16, userID int64) (repo.UnsNode, error) {
	name := topicTypeName(topicType)
	if name == "" {
		return repo.UnsNode{}, ErrInvalid
	}
	nodes, err := s.uns.ListUnsNodes(ctx, repo.UnsNodeFilter{ParentID: parentID, ParentIDSet: true})
	if err != nil {
		return repo.UnsNode{}, err
	}
	for _, node := range nodes {
		if !strings.EqualFold(node.Name, name) {
			continue
		}
		if node.Type != 1 || node.TopicType != topicType {
			return repo.UnsNode{}, ErrInvalid
		}
		return node, nil
	}
	resp, err := s.createSingle(ctx, SaveCommand{
		ParentID:          parentID,
		Name:              name,
		DisplayName:       name,
		NodeType:          "folder",
		TopicType:         name,
		UserID:            userID,
		SystemTopicFolder: true,
	})
	if err != nil {
		return repo.UnsNode{}, err
	}
	return s.uns.GetUnsNode(ctx, toInt64(resp["id"]), false)
}

func (s *Service) createByNamespacePath(ctx context.Context, cmd SaveCommand) (map[string]any, bool, error) {
	fullPath := strings.Trim(cmd.Namespace, "/")
	if fullPath == "" {
		fullPath = strings.Trim(cmd.Name, "/")
	}
	if !strings.Contains(fullPath, "/") {
		return nil, false, nil
	}
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil {
		return nil, true, err
	}
	parts := splitNamespaceParts(fullPath)
	if len(parts) == 0 {
		return nil, true, ErrInvalid
	}
	if err := validateCreatePathParts(fullPath, parts, nodeType); err != nil {
		return nil, true, err
	}
	parentID := cmd.ParentID
	if nodeType == 2 {
		if len(parts) < 2 {
			return nil, true, ErrInvalid
		}
		topicType, err := topicTypeValue(cmd.TopicType)
		if err != nil {
			return nil, true, err
		}
		inferred := systemTopicTypeByName(parts[len(parts)-2])
		if inferred == 0 {
			return nil, true, ErrInvalid
		}
		if topicType != 0 && topicType != inferred {
			return nil, true, ErrInvalid
		}
		topicType = inferred
		folderParts := parts[:len(parts)-2]
		for _, part := range folderParts {
			folder, err := s.ensurePathFolder(ctx, parentID, part, cmd.UserID)
			if err != nil {
				return nil, true, err
			}
			parentID = folder.ID
		}
		cmd.ParentID = parentID
		cmd.Name = parts[len(parts)-1]
		cmd.Namespace = ""
		cmd.TopicType = topicTypeName(topicType)
		resp, err := s.createSingle(ctx, cmd)
		return resp, true, err
	}
	for _, part := range parts[:len(parts)-1] {
		folder, err := s.ensurePathFolder(ctx, parentID, part, cmd.UserID)
		if err != nil {
			return nil, true, err
		}
		parentID = folder.ID
	}
	cmd.ParentID = parentID
	cmd.Name = parts[len(parts)-1]
	cmd.Namespace = ""
	resp, err := s.createSingle(ctx, cmd)
	return resp, true, err
}

func (s *Service) validateParentRule(ctx context.Context, node repo.UnsNode) error {
	if node.ParentID == 0 {
		return nil
	}
	parent, err := s.uns.GetUnsNode(ctx, node.ParentID, false)
	if err != nil {
		return err
	}
	if parent.Type != 1 {
		return ErrInvalid
	}
	if parent.TopicType == 0 {
		return nil
	}
	if node.Type == 1 {
		return ErrInvalid
	}
	if node.Type == 2 && node.TopicType != 0 && node.TopicType != parent.TopicType {
		return ErrInvalid
	}
	return nil
}

func (s *Service) ensurePathFolder(ctx context.Context, parentID int64, name string, userID int64) (repo.UnsNode, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return repo.UnsNode{}, ErrInvalid
	}
	nodes, err := s.uns.ListUnsNodes(ctx, repo.UnsNodeFilter{ParentID: parentID, ParentIDSet: true})
	if err != nil {
		return repo.UnsNode{}, err
	}
	for _, node := range nodes {
		if node.Type == 1 && strings.EqualFold(node.Name, name) {
			return node, nil
		}
	}
	resp, err := s.createSingle(ctx, SaveCommand{
		ParentID:    parentID,
		Name:        name,
		DisplayName: name,
		NodeType:    "folder",
		UserID:      userID,
	})
	if err != nil {
		return repo.UnsNode{}, err
	}
	return s.uns.GetUnsNode(ctx, toInt64(resp["id"]), false)
}

func (s *Service) Update(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	current, err := s.uns.GetUnsNode(ctx, cmd.ID, false)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	cmd.Alias = current.Alias
	cmd.PreserveAlias = true
	if cmd.DataQualityPolicy == nil {
		cmd.DataQualityPolicy = current.DataQualityPolicy
	}
	if current.Type == 1 && current.TopicType != 0 && strings.EqualFold(current.Name, topicTypeName(current.TopicType)) {
		cmd.SystemTopicFolder = true
		if strings.TrimSpace(cmd.TopicType) == "" {
			cmd.TopicType = topicTypeName(current.TopicType)
		}
	}
	node, err := s.commandToNode(ctx, cmd)
	if err != nil {
		return nil, err
	}
	node.ID = cmd.ID
	node.UpdatedBy = cmd.UserID
	if err := s.publishInTx(ctx, events.UnsNodeSaving{Action: "update", Node: node, UserID: cmd.UserID}); err != nil {
		return nil, err
	}
	updated, err := s.uns.UpdateUnsNode(ctx, node, cmd.LabelIDs, cmd.AssetFileIDs)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	s.publishPostCommit(ctx, events.UnsNodeUpdated{Node: updated})
	return nodeResp(updated), nil
}

// UpdateFromConsole shares the ordinary UNS update rules.
func (s *Service) UpdateFromConsole(ctx context.Context, cmd SaveCommand) (map[string]any, error) {
	return s.Update(ctx, cmd)
}

func (s *Service) Delete(ctx context.Context, id, userID int64) (map[string]any, error) {
	if err := s.publishInTx(ctx, events.UnsNodeDeleting{NodeID: id, UserID: userID}); err != nil {
		return nil, err
	}
	nodes, err := s.uns.DeleteUnsNode(ctx, id, userID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	s.publishPostCommit(ctx, events.UnsNodeDeleted{RootID: id, UserID: userID, Nodes: nodes})
	resp := map[string]any{"deleted": len(nodes), "nodes": nodesResp(nodes)}
	addRootNodeResp(resp, id, nodes)
	return resp, nil
}

func (s *Service) Restore(ctx context.Context, id, userID int64, confirm bool) (map[string]any, error) {
	if err := s.publishInTx(ctx, events.UnsNodeRestoring{NodeID: id, UserID: userID, Confirm: confirm}); err != nil {
		return nil, err
	}
	nodes, err := s.uns.RestoreUnsNode(ctx, id, userID, confirm)
	if err != nil {
		if errors.Is(err, repo.ErrUnsRestoreConflict) {
			return nil, ErrRestoreConflict
		}
		return nil, normalizeNotFound(err)
	}
	s.publishPostCommit(ctx, events.UnsNodeRestored{RootID: id, UserID: userID, Nodes: nodes})
	resp := map[string]any{"restored": len(nodes), "nodes": nodesResp(nodes)}
	addRootNodeResp(resp, id, nodes)
	return resp, nil
}

func (s *Service) ForceDelete(ctx context.Context, id, userID int64, deleteFlow bool) (map[string]any, error) {
	if err := s.publishInTx(ctx, events.UnsNodeForceDeleting{NodeID: id, UserID: userID, DeleteFlow: deleteFlow}); err != nil {
		return nil, err
	}
	nodes, err := s.uns.ForceDeleteUnsNode(ctx, id, userID)
	if err != nil {
		if errors.Is(err, repo.ErrUnsNotInRecycle) {
			return nil, ErrNotInRecycle
		}
		return nil, normalizeNotFound(err)
	}
	var deletedFlows []map[string]any
	s.publishPostCommit(ctx, events.UnsNodeForceDeleted{RootID: id, UserID: userID, DeleteFlow: deleteFlow, Nodes: nodes})
	resp := map[string]any{"forceDeleted": len(nodes), "nodes": nodesResp(nodes)}
	if deleteFlow {
		resp["deletedFlows"] = len(deletedFlows)
		resp["flows"] = deletedFlows
	}
	addRootNodeResp(resp, id, nodes)
	return resp, nil
}

func addRootNodeResp(resp map[string]any, rootID int64, nodes []repo.UnsNode) {
	if resp == nil || rootID <= 0 {
		return
	}
	for _, node := range nodes {
		if node.ID != rootID {
			continue
		}
		for key, value := range nodeResp(node) {
			if _, exists := resp[key]; !exists {
				resp[key] = value
			}
		}
		return
	}
}

func (s *Service) Templates(ctx context.Context) (map[string]any, error) {
	items, err := s.uns.ListUnsTemplates(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]any, 0, len(items))
	for _, item := range items {
		list = append(list, templateResp(item))
	}
	return map[string]any{"list": list, "total": len(list)}, nil
}

func (s *Service) TemplateDetail(ctx context.Context, id int64) (map[string]any, error) {
	item, err := s.uns.GetUnsTemplate(ctx, id)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return templateResp(item), nil
}

func (s *Service) CreateTemplate(ctx context.Context, cmd TemplateCommand) (map[string]any, error) {
	item, err := s.templateCommandToRepo(cmd)
	if err != nil {
		return nil, err
	}
	item.CreatedBy = cmd.UserID
	created, err := s.uns.CreateUnsTemplate(ctx, item)
	if err != nil {
		return nil, err
	}
	return templateResp(created), nil
}

func (s *Service) UpdateTemplate(ctx context.Context, cmd TemplateCommand) (map[string]any, error) {
	current, err := s.uns.GetUnsTemplate(ctx, cmd.ID)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	item, err := s.templateCommandToRepo(cmd)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(item.Name) == "" {
		item.Name = current.Name
	}
	item.UpdatedBy = cmd.UserID
	updated, err := s.uns.UpdateUnsTemplate(ctx, cmd.ID, item)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return templateResp(updated), nil
}

func (s *Service) DeleteTemplate(ctx context.Context, id, userID int64) (map[string]any, error) {
	if err := s.uns.DeleteUnsTemplate(ctx, id, userID); err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"deleted": true}, nil
}

func (s *Service) Labels(ctx context.Context) (map[string]any, error) {
	items, err := s.uns.ListUnsLabels(ctx)
	if err != nil {
		return nil, err
	}
	list := make([]any, 0, len(items))
	for _, item := range items {
		list = append(list, labelResp(item))
	}
	return map[string]any{"list": list, "total": len(list)}, nil
}

func (s *Service) LabelDetail(ctx context.Context, id int64) (map[string]any, error) {
	item, err := s.uns.GetUnsLabel(ctx, id)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return labelResp(item), nil
}

func (s *Service) CreateLabel(ctx context.Context, cmd LabelCommand) (map[string]any, error) {
	name := strings.TrimSpace(cmd.Name)
	description := strings.TrimSpace(cmd.Description)
	if name == "" || domaincommon.ExceedNameLimit(name) || domaincommon.ExceedDescLimit(description) {
		return nil, ErrInvalid
	}
	item, err := s.uns.CreateUnsLabel(ctx, repo.UnsLabel{
		Name:        name,
		Color:       strings.TrimSpace(cmd.Color),
		Description: description,
	})
	if err != nil {
		return nil, err
	}
	return labelResp(item), nil
}

func (s *Service) UpdateLabel(ctx context.Context, cmd LabelCommand) (map[string]any, error) {
	name := strings.TrimSpace(cmd.Name)
	description := strings.TrimSpace(cmd.Description)
	if name == "" || domaincommon.ExceedNameLimit(name) || domaincommon.ExceedDescLimit(description) {
		return nil, ErrInvalid
	}
	item, err := s.uns.UpdateUnsLabel(ctx, cmd.ID, repo.UnsLabel{
		Name:        name,
		Color:       strings.TrimSpace(cmd.Color),
		Description: description,
	})
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return labelResp(item), nil
}

func (s *Service) DeleteLabel(ctx context.Context, id int64) (map[string]any, error) {
	if err := s.uns.DeleteUnsLabel(ctx, id); err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"deleted": true}, nil
}

func (s *Service) CreateImportJob(ctx context.Context, dryRun bool, sourceFileID int64, userID int64) (map[string]any, error) {
	if sourceFileID <= 0 {
		return nil, ErrInvalid
	}
	var job repo.AsyncJob
	err := repo.GetCommonConn(ctx).WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		job, err = repo.NewAsyncJobRepo(tx).CreateAsyncJob(ctx, "uns.import", map[string]any{"dryRun": dryRun, "sourceFileId": sourceFileID}, userID)
		if err != nil {
			return err
		}
		return s.outbox.EnqueueOnTx(tx, "uns.import.job", "asyncJob", job.JobKey, job)
	})
	if err != nil {
		return nil, err
	}
	return map[string]any{"job": job}, nil
}

func (s *Service) CreateExportJob(ctx context.Context, rootNodeID int64, userID int64) (map[string]any, error) {
	job, err := s.jobs.CreateAsyncJob(ctx, "uns.export", map[string]any{"rootNodeId": rootNodeID}, userID)
	if err != nil {
		return nil, err
	}
	if err := s.outbox.Enqueue(ctx, "uns.export.job", "asyncJob", job.JobKey, job); err != nil {
		_ = s.jobs.MarkAsyncJobFailed(ctx, job.JobKey, err.Error(), map[string]any{"error": err.Error()})
		return nil, err
	}
	return map[string]any{"job": job}, nil
}

func (s *Service) ExportData(ctx context.Context, cmd ExportCommand) (map[string]any, error) {
	nodes, err := s.uns.ListUnsNodes(ctx, repo.UnsNodeFilter{})
	if err != nil {
		return nil, err
	}
	nodes = filterExportNodes(nodes, cmd)
	return map[string]any{
		"notes":     "type:PATH|TOPIC,topicType:STATE|ACTION|METRIC,fields.type:INTEGER|STRING|FLOAT|DOUBLE|BOOLEAN|LONG|DATETIME",
		"namespace": exportNamespaceTree(nodes),
	}, nil
}

func (s *Service) ImportData(ctx context.Context, fileName string, raw []byte, userID int64) (map[string]any, error) {
	return s.ImportDataStream(ctx, fileName, raw, userID, nil)
}

func (s *Service) ValidateImportData(ctx context.Context, fileName string, raw []byte, statusConsumer func(ImportStatus)) (map[string]any, error) {
	_ = ctx
	emit := func(status ImportStatus) {
		status.Module = "uns"
		if status.Code == 0 {
			status.Code = 200
		}
		if statusConsumer != nil {
			statusConsumer(status)
		}
	}
	emit(ImportStatus{Task: "Start Import Dry Run", Msg: "start", Progress: 1})
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileName)), ".json") {
		emit(ImportStatus{Code: 500, Msg: ErrInvalidImportFileType.Error(), Task: "Parse File", Finished: true, Progress: 100})
		return nil, ErrInvalidImportFileType
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		emit(ImportStatus{Code: 500, Msg: ErrInvalidJSON.Error(), Task: "Parse JSON", Finished: true, Progress: 100})
		return nil, ErrInvalidJSON
	}
	if !hasImportNamespacePayload(payload) {
		emit(ImportStatus{Code: 500, Msg: ErrInvalidImportFormat.Error(), Task: "Parse JSON", Finished: true, Progress: 100})
		return nil, ErrInvalidImportFormat
	}
	labelCount := len(importArray(payload, "labels", "label", "Label"))
	nodeCount := len(extractImportNodes(payload))
	if tree := importTree(payload); len(tree) > 0 {
		nodeCount = countImportTreeNodes(tree)
	}
	result := map[string]any{
		"dryRun": true,
		"valid":  true,
		"labels": labelCount,
		"nodes":  nodeCount,
	}
	emit(ImportStatus{Code: 200, Msg: "ok", Task: "Finish Import Dry Run", Finished: true, Progress: 100, Imported: nodeCount})
	return result, nil
}

func (s *Service) ImportDataStream(ctx context.Context, fileName string, raw []byte, userID int64, statusConsumer func(ImportStatus)) (map[string]any, error) {
	return s.importData(ctx, fileName, raw, userID, "", statusConsumer)
}

// ImportJob commits the result with the nodes so redelivery cannot import twice.
func (s *Service) ImportJob(ctx context.Context, fileName string, raw []byte, userID int64, jobKey string, statusConsumer func(ImportStatus)) (map[string]any, error) {
	return s.importData(ctx, fileName, raw, userID, jobKey, statusConsumer)
}

func (s *Service) importData(ctx context.Context, fileName string, raw []byte, userID int64, jobKey string, statusConsumer func(ImportStatus)) (map[string]any, error) {
	emit := func(status ImportStatus) {
		status.Module = "uns"
		if status.Code == 0 {
			status.Code = 200
		}
		if statusConsumer != nil {
			statusConsumer(status)
		}
	}
	emit(ImportStatus{Task: "Start Import", Msg: "start", Progress: 1})
	if !strings.HasSuffix(strings.ToLower(strings.TrimSpace(fileName)), ".json") {
		emit(ImportStatus{Code: 500, Msg: ErrInvalidImportFileType.Error(), Task: "Parse File", Finished: true, Progress: 100})
		return nil, ErrInvalidImportFileType
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		emit(ImportStatus{Code: 500, Msg: ErrInvalidJSON.Error(), Task: "Parse JSON", Finished: true, Progress: 100})
		return nil, ErrInvalidJSON
	}
	if !hasImportNamespacePayload(payload) {
		emit(ImportStatus{Code: 500, Msg: ErrInvalidImportFormat.Error(), Task: "Parse JSON", Finished: true, Progress: 100})
		return nil, ErrInvalidImportFormat
	}
	result, err := s.importBatch(ctx, payload, userID, jobKey, emit)
	if err != nil {
		emit(ImportStatus{Code: 500, Msg: err.Error(), Task: "Namespace Import", Finished: true, Progress: 100})
		return nil, err
	}
	emit(ImportStatus{Code: 200, Msg: "ok", Task: "Finish Import", Finished: true, Progress: 100, Imported: int(toInt64(result["imported"])), Skipped: int(toInt64(result["skipped"]))})
	return result, nil
}

func (s *Service) importNamespacePayload(ctx context.Context, payload map[string]any, userID int64, emit func(ImportStatus), batch *importBatchState) (int, int, error) {
	if len(importTree(payload)) > 0 {
		return s.importNamespaceTree(ctx, payload, userID, emit, batch)
	}
	rawNodes := extractImportNodes(payload)
	if len(rawNodes) == 0 {
		return 0, 0, nil
	}
	current := batch.existing
	byNamespace := map[string]repo.UnsNode{}
	byAlias := map[string]repo.UnsNode{}
	for _, node := range current {
		byNamespace[node.Namespace] = node
		indexImportAliasNode(byAlias, node)
	}
	sort.SliceStable(rawNodes, func(i, j int) bool {
		di := strings.Count(strings.Trim(rawNodes[i].Namespace, "/"), "/")
		dj := strings.Count(strings.Trim(rawNodes[j].Namespace, "/"), "/")
		if di == dj {
			return rawNodes[i].Type < rawNodes[j].Type
		}
		return di < dj
	})
	imported := 0
	skipped := 0
	total := len(rawNodes)
	for index, node := range rawNodes {
		namespace := strings.Trim(node.Namespace, "/")
		if namespace == "" {
			continue
		}
		parentID := int64(0)
		name := lastNamespacePart(namespace)
		var parent repo.UnsNode
		hasParent := false
		if parentNamespace := parentNamespace(namespace); parentNamespace != "" {
			if found, exists := byNamespace[parentNamespace]; exists {
				parent = found
				hasParent = true
				parentID = parent.ID
			} else {
				name = namespace
			}
		}
		nameTopicType := importSystemTopicTypeByName(name)
		systemTopicFolder := false
		if node.Type == 0 {
			if hasParent && parent.TopicType != 0 {
				node.Type = 2
			} else {
				node.Type = 1
			}
		}
		if node.Type == 1 {
			if hasParent && parent.TopicType != 0 {
				if nameTopicType == parent.TopicType {
					byNamespace[namespace] = parent
					skipped++
					continue
				}
				return imported, skipped, ErrInvalid
			}
			if nameTopicType != 0 {
				if node.TopicType != 0 && node.TopicType != nameTopicType {
					return imported, skipped, ErrInvalid
				}
				node.TopicType = nameTopicType
				systemTopicFolder = true
			} else if node.TopicType != 0 {
				return imported, skipped, ErrInvalid
			}
		}
		if node.Type == 2 {
			if hasParent && parent.TopicType != 0 {
				if node.TopicType == 0 {
					node.TopicType = parent.TopicType
				} else if node.TopicType != parent.TopicType {
					return imported, skipped, ErrInvalid
				}
			} else if node.TopicType == 0 {
				node.TopicType = importTopicTypeFromNamespace(namespace)
			}
			if node.TopicType == 0 {
				return imported, skipped, ErrInvalid
			}
		}
		cmd := SaveCommand{
			ParentID:          parentID,
			Name:              name,
			DisplayName:       node.DisplayName,
			Description:       node.Description,
			Alias:             node.Alias,
			PreserveAlias:     strings.TrimSpace(node.Alias) != "",
			NodeType:          nodeTypeName(node.Type),
			TopicType:         topicTypeName(node.TopicType),
			Schema:            string(node.Schema),
			ExtendProperties:  string(node.ExtendProperties),
			DataQualityPolicy: node.DataQualityPolicy,
			EnableHistory:     boolPtr(node.EnableHistory == 1),
			MockData:          node.MockData == 1,
			UserID:            userID,
			SystemTopicFolder: systemTopicFolder,
		}
		created, err := batch.prepare(cmd)
		if err != nil {
			return imported, skipped, err
		}
		indexImportNode(byNamespace, created)
		if parent, ok := batch.tree.byID[created.ParentID]; ok {
			indexImportNode(byNamespace, parent)
		}
		indexImportAliasNode(byAlias, created)
		imported++
		if emit != nil {
			emit(ImportStatus{Task: "Namespace Import", Progress: 40 + float64(index+1)*55/float64(max(total, 1)), Imported: imported, Skipped: skipped})
		}
	}
	return imported, skipped, nil
}

func hasImportNamespacePayload(payload map[string]any) bool {
	return len(extractImportNodes(payload)) > 0 || len(importTree(payload)) > 0
}

func countImportTreeNodes(items []importFileData) int {
	total := 0
	for _, item := range items {
		total++
		total += countImportTreeNodes(item.Children)
	}
	return total
}

func (s *Service) Job(ctx context.Context, jobKey string) (map[string]any, error) {
	job, err := s.jobs.GetAsyncJob(ctx, jobKey)
	if err != nil {
		return nil, normalizeNotFound(err)
	}
	return map[string]any{"job": job}, nil
}

func filterExportNodes(nodes []repo.UnsNode, cmd ExportCommand) []repo.UnsNode {
	if strings.EqualFold(cmd.ExportType, "ALL") || (len(cmd.Folders) == 0 && len(cmd.Files) == 0) {
		return nodes
	}
	folders := map[int64]repo.UnsNode{}
	files := map[int64]struct{}{}
	for _, id := range cmd.Files {
		if id > 0 {
			files[id] = struct{}{}
		}
	}
	for _, id := range cmd.Folders {
		if id <= 0 {
			continue
		}
		for _, node := range nodes {
			if node.ID == id {
				folders[id] = node
				break
			}
		}
	}
	out := make([]repo.UnsNode, 0, len(nodes))
	seen := map[int64]struct{}{}
	for _, node := range nodes {
		if _, ok := files[node.ID]; ok {
			out = append(out, node)
			seen[node.ID] = struct{}{}
			continue
		}
		for _, folder := range folders {
			if node.ID == folder.ID || strings.HasPrefix(node.Namespace, folder.Namespace+"/") {
				if _, exists := seen[node.ID]; !exists {
					out = append(out, node)
					seen[node.ID] = struct{}{}
				}
				break
			}
		}
	}
	return out
}

func extractImportNodes(payload map[string]any) []repo.UnsNode {
	raw := payload["nodes"]
	if unsPayload, ok := payload["uns"].(map[string]any); ok {
		raw = unsPayload["nodes"]
	}
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	nodes := make([]repo.UnsNode, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		schema, _ := json.Marshal(schemaFieldsFromAny(firstNonNil(m["fields"], m["schema"])))
		extend, _ := json.Marshal(defaultMap(m["extendProperties"]))
		dataQualityPolicy := importDataQualityPolicy(m["dataQualityPolicy"])
		nodes = append(nodes, repo.UnsNode{
			Namespace:         strings.Trim(importString(m, "namespace"), "/"),
			DisplayName:       importString(m, "displayName"),
			Description:       importString(m, "description"),
			Alias:             importString(m, "alias"),
			Type:              importNodeType(m["type"]),
			TopicType:         importTopicType(m["topicType"]),
			Schema:            schema,
			ExtendProperties:  extend,
			DataQualityPolicy: dataQualityPolicy,
			EnableHistory:     yesNoFlag(parseImportBool(importString(m, "enableHistory"))),
			MockData:          yesNoFlag(parseImportBool(importString(m, "mockData"))),
		})
	}
	return nodes
}

func (s *Service) importLabels(ctx context.Context, payload map[string]any, userID int64, emit func(ImportStatus)) (int, int, error) {
	imported := 0
	skipped := 0
	labelItems := importArray(payload, "labels", "label", "Label")
	if len(labelItems) > 0 {
		labels, err := s.uns.ListUnsLabels(ctx)
		if err != nil {
			return imported, skipped, err
		}
		exists := map[string]struct{}{}
		for _, label := range labels {
			exists[strings.ToLower(strings.TrimSpace(label.Name))] = struct{}{}
		}
		for index, item := range labelItems {
			name := importString(item, "name")
			if name == "" {
				skipped++
				continue
			}
			if _, ok := exists[strings.ToLower(name)]; ok {
				skipped++
				continue
			}
			if _, err := s.CreateLabel(ctx, LabelCommand{
				Name:        name,
				Color:       importString(item, "color"),
				Description: importString(item, "description"),
				UserID:      userID,
			}); err != nil {
				return imported, skipped, err
			}
			exists[strings.ToLower(name)] = struct{}{}
			imported++
			if emit != nil {
				emit(ImportStatus{Task: "Label Import", Progress: 5 + float64(index+1)*10/float64(max(len(labelItems), 1)), Imported: imported, Skipped: skipped})
			}
		}
	}

	return imported, skipped, nil
}

func (s *Service) importNamespaceTree(ctx context.Context, payload map[string]any, userID int64, emit func(ImportStatus), batch *importBatchState) (int, int, error) {
	tree := importTree(payload)
	if len(tree) == 0 {
		return 0, 0, nil
	}
	labelIDs := batch.labelIDs
	current := batch.existing
	byNamespace := map[string]repo.UnsNode{}
	byAlias := map[string]repo.UnsNode{}
	for _, node := range current {
		byNamespace[strings.Trim(node.Namespace, "/")] = node
		indexImportAliasNode(byAlias, node)
	}
	// An alias may refer to the same existing node, but cannot occur twice in a file.
	if err := validateImportTreeAliases(tree); err != nil {
		return 0, 0, err
	}
	total := countImportTree(tree)
	imported := 0
	skipped := 0
	processed := 0
	var walk func(items []importFileData, parentID int64, parentNamespace string, inheritedTopic string, parentTopicType int16) error
	walk = func(items []importFileData, parentID int64, parentNamespace string, inheritedTopic string, parentTopicType int16) error {
		for _, item := range items {
			processed++
			name := strings.Trim(strings.TrimSpace(item.Name), "/")
			if name == "" {
				skipped++
				continue
			}
			nodeType := importNodeTypeNameForParent(item.Type, parentTopicType)
			topicType := ""
			systemTopicFolder := false
			if nodeType == "folder" {
				nameTopicType := importSystemTopicTypeByName(name)
				explicitTopicType := importExplicitTopicType(item)
				switch {
				case nameTopicType != 0:
					if explicitTopicType != 0 && explicitTopicType != nameTopicType {
						return ErrInvalid
					}
					if parentTopicType == nameTopicType {
						skipped++
						if err := walk(item.Children, parentID, parentNamespace, topicTypeName(parentTopicType), parentTopicType); err != nil {
							return err
						}
						continue
					}
					if parentTopicType != 0 {
						return ErrInvalid
					}
					systemTopicFolder = true
					topicType = topicTypeName(nameTopicType)
				case parentTopicType != 0:
					return ErrInvalid
				case explicitTopicType != 0:
					return ErrInvalid
				default:
					topicType = ""
				}
			} else if parentTopicType != 0 {
				topicType = topicTypeName(parentTopicType)
			} else {
				topicType = importTopicTypeNameFromFile(item, inheritedTopic)
				if topicType == "" {
					return ErrInvalid
				}
			}
			fields := importItemFields(item)
			cmdTopicType := ""
			if nodeType == "file" || systemTopicFolder {
				cmdTopicType = topicType
			}
			cmd := SaveCommand{
				ParentID:          parentID,
				Name:              name,
				DisplayName:       firstNonEmpty(item.DisplayName, name),
				Description:       item.Description,
				Alias:             item.Alias,
				PreserveAlias:     true,
				NodeType:          nodeType,
				TopicType:         cmdTopicType,
				Schema:            importSchemaJSON(item, fields, topicType),
				ExtendProperties:  importExtendJSON(item),
				DataQualityPolicy: normalizeImportedDataQualityPolicy(item.DataQualityPolicy),
				LabelIDs:          importNodeLabelIDs(item, labelIDs),
				EnableHistory:     boolPtr(parseImportBool(item.EnableHistory)),
				AddFlow:           parseImportBool(item.MockData),
				MockData:          parseImportBool(item.MockData),
				UserID:            userID,
				SystemTopicFolder: systemTopicFolder,
			}
			created, err := batch.prepare(cmd)
			if err != nil {
				return err
			}
			indexImportNode(byNamespace, created)
			if parent, ok := batch.tree.byID[created.ParentID]; ok {
				indexImportNode(byNamespace, parent)
			}
			indexImportAliasNode(byAlias, created)
			imported++
			if emit != nil {
				emit(ImportStatus{Task: "Namespace Import", Progress: 40 + float64(processed)*55/float64(max(total, 1)), Imported: imported, Skipped: skipped})
			}
			nextTopic := inheritedTopic
			nextParentTopicType := int16(0)
			if created.TopicType != 0 {
				nextTopic = topicTypeName(created.TopicType)
				nextParentTopicType = created.TopicType
			} else if topicType != "" {
				nextTopic = topicType
			}
			if err := walk(item.Children, created.ID, created.Namespace, nextTopic, nextParentTopicType); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(tree, 0, "", "", 0); err != nil {
		return imported, skipped, err
	}
	return imported, skipped, nil
}

func validateImportTreeAliases(items []importFileData) error {
	seen := make(map[string]struct{})
	var walk func([]importFileData) error
	walk = func(nodes []importFileData) error {
		for _, item := range nodes {
			alias := normalizeAliasCandidate(item.Alias)
			if alias != "" {
				if !isAliasFormatOK(alias) {
					return fmt.Errorf("invalid alias: alias=%s", alias)
				}
				if _, exists := seen[alias]; exists {
					return fmt.Errorf("alias already exists: alias=%s", alias)
				}
				seen[alias] = struct{}{}
			}
			if err := walk(item.Children); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(items)
}

func (s *Service) indexImportCreatedNode(ctx context.Context, byNamespace map[string]repo.UnsNode, created repo.UnsNode, requestedParentID int64) error {
	indexImportNode(byNamespace, created)
	if created.ParentID == 0 || created.ParentID == requestedParentID {
		return nil
	}
	parent, err := s.uns.GetUnsNode(ctx, created.ParentID, false)
	if err != nil {
		if errors.Is(err, repo.ErrNotFound) {
			return nil
		}
		return err
	}
	indexImportNode(byNamespace, parent)
	return nil
}

func indexImportNode(byNamespace map[string]repo.UnsNode, node repo.UnsNode) {
	namespace := strings.Trim(node.Namespace, "/")
	if namespace == "" {
		return
	}
	byNamespace[namespace] = node
}

func indexImportAliasNode(byAlias map[string]repo.UnsNode, node repo.UnsNode) {
	alias := normalizeAliasCandidate(node.Alias)
	if alias == "" {
		return
	}
	byAlias[alias] = node
}

func validateImportAliasCandidate(alias string, byAlias map[string]repo.UnsNode) (string, error) {
	alias = normalizeAliasCandidate(alias)
	if alias == "" {
		return "", nil
	}
	if !isAliasFormatOK(alias) {
		return "", fmt.Errorf("invalid alias: alias=%s", alias)
	}
	if existing, ok := byAlias[alias]; ok && existing.ID > 0 {
		return "", fmt.Errorf("alias already exists: alias=%s", alias)
	}
	return alias, nil
}

func importArray(payload map[string]any, keys ...string) []map[string]any {
	for _, key := range keys {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		items, ok := raw.([]any)
		if !ok {
			continue
		}
		out := make([]map[string]any, 0, len(items))
		for _, item := range items {
			if m, ok := item.(map[string]any); ok {
				out = append(out, m)
			}
		}
		return out
	}
	return nil
}

func importString(item map[string]any, key string) string {
	value, ok := item[key]
	if !ok || value == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(value))
}

func importTree(payload map[string]any) []importFileData {
	for _, key := range []string{"namespace", "UNS", "uns"} {
		raw, ok := payload[key]
		if !ok {
			continue
		}
		if m, ok := raw.(map[string]any); ok {
			raw = m["nodes"]
			if strings.EqualFold(key, "uns") && isRawNamespaceNodeArray(raw) {
				continue
			}
		}
		data, _ := json.Marshal(raw)
		var tree []importFileData
		if err := json.Unmarshal(data, &tree); err == nil && len(tree) > 0 {
			return tree
		}
	}
	return nil
}

func isRawNamespaceNodeArray(raw any) bool {
	items, ok := raw.([]any)
	if !ok {
		return false
	}
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if _, ok := m["namespace"]; ok {
			return true
		}
	}
	return false
}

func importFields(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name := importString(m, "name")
		if name == "" {
			continue
		}
		field := map[string]any{"name": name, "type": strings.ToUpper(importString(m, "type"))}
		if displayName := importString(m, "displayName"); displayName != "" {
			field["displayName"] = displayName
		}
		if maxLen := toInt64(m["maxLen"]); maxLen > 0 {
			field["maxLen"] = maxLen
		}
		out = append(out, field)
	}
	return out
}

func (s *Service) importLabelIDs(ctx context.Context) (map[string]int64, error) {
	labels, err := s.uns.ListUnsLabels(ctx)
	if err != nil {
		return nil, err
	}
	out := map[string]int64{}
	for _, label := range labels {
		out[strings.ToLower(strings.TrimSpace(label.Name))] = label.ID
	}
	return out, nil
}

func importNodeLabelIDs(item importFileData, labels map[string]int64) []int64 {
	raw := item.Label
	if raw == "" && item.Labels != nil {
		switch v := item.Labels.(type) {
		case string:
			raw = v
		case []any:
			parts := make([]string, 0, len(v))
			for _, part := range v {
				parts = append(parts, fmt.Sprint(part))
			}
			raw = strings.Join(parts, ",")
		}
	}
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	out := []int64{}
	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if id := labels[name]; id > 0 {
			out = append(out, id)
		}
	}
	return out
}

func countImportTree(items []importFileData) int {
	total := 0
	for _, item := range items {
		total++
		total += countImportTree(item.Children)
	}
	return total
}

func importNodeTypeName(value string) string {
	return importNodeTypeNameForParent(value, 0)
}

func importNodeTypeNameForParent(value string, parentTopicType int16) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "topic", "file", "2":
		return "file"
	case "", "0":
		if parentTopicType != 0 {
			return "file"
		}
		return "folder"
	default:
		return "folder"
	}
}

func importTopicTypeName(item map[string]any) string {
	for _, key := range []string{"topicType", "parentDataType", "dataType"} {
		value := normalizeImportTopicType(importString(item, key))
		if value != "" {
			return value
		}
	}
	for _, key := range []string{"alias", "name"} {
		if value := topicTypeName(importSystemTopicTypeByName(importString(item, key))); value != "" {
			return value
		}
	}
	return "State"
}

func importTopicTypeNameFromFile(item importFileData, inherited string) string {
	for _, value := range []string{item.TopicType, item.ParentDataType, fmt.Sprint(item.DataType), inherited} {
		if normalized := normalizeImportTopicType(value); normalized != "" {
			return normalized
		}
	}
	if topicType := importSystemTopicTypeByName(item.Name); topicType != 0 {
		return topicTypeName(topicType)
	}
	return ""
}

func importExplicitTopicType(item importFileData) int16 {
	for _, value := range []string{item.TopicType, item.ParentDataType, fmt.Sprint(item.DataType)} {
		normalized := normalizeImportTopicType(value)
		if normalized == "" {
			continue
		}
		topicType, err := topicTypeValue(normalized)
		if err == nil {
			return topicType
		}
	}
	return 0
}

func validateImportExistingNode(node repo.UnsNode, nodeType string, systemTopicFolder bool, systemTopicType int16) error {
	expectedType, err := nodeTypeValue(nodeType)
	if err != nil {
		return err
	}
	if node.Type != expectedType {
		return ErrInvalid
	}
	// Older trees kept State, Action, and Metric grouping folders at topic_type=0.
	// Treat zero as unspecified while still rejecting a conflicting non-zero type.
	if systemTopicFolder && node.TopicType != 0 && node.TopicType != systemTopicType {
		return ErrInvalid
	}
	return nil
}

func importExistingNodeTopicContext(node repo.UnsNode, inheritedTopic, topicType string, systemTopicFolder bool, systemTopicType int16) (string, int16) {
	if node.TopicType != 0 {
		return topicTypeName(node.TopicType), node.TopicType
	}
	// Preserve the type inferred from the special folder name for child recursion
	// without mutating the legacy folder row.
	if systemTopicFolder && systemTopicType != 0 {
		return topicTypeName(systemTopicType), systemTopicType
	}
	if topicType != "" {
		return topicType, 0
	}
	return inheritedTopic, 0
}

func normalizeImportTopicType(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "state", "1":
		return "State"
	case "action", "2":
		return "Action"
	case "metric", "3":
		return "Metric"
	default:
		return ""
	}
}

func importSchemaJSON(_ importFileData, fields []map[string]any, _ string) string {
	data, _ := json.Marshal(fields)
	return string(data)
}

func importItemFields(item importFileData) []map[string]any {
	if len(item.Fields) > 0 {
		return item.Fields
	}
	return item.LegacySchema
}

func firstNonNil(values ...any) any {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func importExtendJSON(item importFileData) string {
	extend := item.ExtendProperties
	if len(extend) == 0 {
		extend = item.Extend
	}
	if len(extend) == 0 {
		return "{}"
	}
	data, _ := json.Marshal(extend)
	return string(data)
}

func parseImportBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1", "yes", "y":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func exportNamespaceTree(nodes []repo.UnsNode) []map[string]any {
	nodeMap := map[int64]repo.UnsNode{}
	children := map[int64][]repo.UnsNode{}
	for _, node := range nodes {
		nodeMap[node.ID] = node
		children[node.ParentID] = append(children[node.ParentID], node)
	}
	roots := children[0]
	for _, node := range nodes {
		if node.ParentID != 0 {
			if _, ok := nodeMap[node.ParentID]; !ok {
				roots = append(roots, node)
			}
		}
	}
	sort.SliceStable(roots, func(i, j int) bool { return roots[i].Namespace < roots[j].Namespace })
	var walk func(repo.UnsNode) map[string]any
	walk = func(node repo.UnsNode) map[string]any {
		item := exportNamespaceNode(node)
		nodeChildren := children[node.ID]
		if len(nodeChildren) > 0 {
			sort.SliceStable(nodeChildren, func(i, j int) bool { return nodeChildren[i].Namespace < nodeChildren[j].Namespace })
			items := make([]map[string]any, 0, len(nodeChildren))
			for _, child := range nodeChildren {
				items = append(items, walk(child))
			}
			item["children"] = items
		}
		return item
	}
	out := make([]map[string]any, 0, len(roots))
	seen := map[int64]struct{}{}
	for _, root := range roots {
		if _, ok := seen[root.ID]; ok {
			continue
		}
		seen[root.ID] = struct{}{}
		out = append(out, walk(root))
	}
	return out
}

func exportNamespaceNode(node repo.UnsNode) map[string]any {
	fields := schemaFieldsFromRaw(node.Schema)
	item := map[string]any{
		"name":        node.Name,
		"type":        "PATH",
		"displayName": node.DisplayName,
		"description": node.Description,
	}
	if node.Type == 2 {
		item["type"] = "TOPIC"
	}
	if alias := unsAlias(node); alias != "" {
		item["alias"] = alias
	}
	if topicType := topicTypeName(node.TopicType); topicType != "" {
		item["topicType"] = strings.ToUpper(topicType)
	}
	if len(fields) > 0 {
		item["fields"] = fields
	}
	if node.EnableHistory == 1 {
		item["enableHistory"] = "TRUE"
	}
	if node.MockData == 1 {
		item["mockData"] = "TRUE"
	}
	extend := rawJSON(node.ExtendProperties)
	if m, ok := extend.(map[string]any); ok && len(m) > 0 {
		item["extendProperties"] = m
	}
	if node.Type == 2 && node.DataQualityPolicy != nil {
		item["dataQualityPolicy"] = node.DataQualityPolicy
	}
	return item
}

func importDataQualityPolicy(value any) *repo.DataQualityPolicy {
	if value == nil {
		return nil
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var policy repo.DataQualityPolicy
	if err := json.Unmarshal(payload, &policy); err != nil {
		return nil
	}
	return normalizeImportedDataQualityPolicy(&policy)
}

func normalizeImportedDataQualityPolicy(policy *repo.DataQualityPolicy) *repo.DataQualityPolicy {
	if policy == nil {
		return nil
	}
	cloned := *policy
	if cloned.MandatoryFields == nil {
		cloned.MandatoryFields = []string{}
	}
	if cloned.FlatFields == nil {
		cloned.FlatFields = []string{}
	}
	if cloned.StaleFields == nil {
		cloned.StaleFields = []string{}
	}
	if cloned.Rules == nil {
		cloned.Rules = []string{}
	}
	if cloned.SeverityOverrides == nil {
		cloned.SeverityOverrides = map[string]string{}
	}
	return &cloned
}

func boolPtr(value bool) *bool {
	return &value
}

func defaultMap(value any) any {
	if value == nil {
		return map[string]any{}
	}
	return value
}

func importNodeType(value any) int16 {
	if value == nil {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(fmt.Sprint(value))) {
	case "", "0", "<nil>":
		return 0
	case "2", "file":
		return 2
	default:
		return 1
	}
}

func importTopicType(value any) int16 {
	v, err := topicTypeValue(fmt.Sprint(value))
	if err != nil {
		return 0
	}
	return v
}

func importSystemTopicTypeByName(value string) int16 {
	return systemTopicTypeByName(value)
}

func systemTopicTypeByName(value string) int16 {
	switch strings.TrimSpace(value) {
	case "State":
		return 1
	case "Action":
		return 2
	case "Metric":
		return 3
	default:
		return 0
	}
}

func importTopicTypeFromNamespace(namespace string) int16 {
	parts := splitNamespaceParts(namespace)
	if len(parts) < 2 {
		return 0
	}
	return importSystemTopicTypeByName(parts[len(parts)-2])
}

func parentNamespace(namespace string) string {
	namespace = strings.Trim(namespace, "/")
	idx := strings.LastIndex(namespace, "/")
	if idx < 0 {
		return ""
	}
	return namespace[:idx]
}

func splitNamespaceParts(namespace string) []string {
	raw := strings.Split(strings.Trim(namespace, "/"), "/")
	parts := make([]string, 0, len(raw))
	for _, part := range raw {
		part = strings.TrimSpace(part)
		if part != "" {
			parts = append(parts, part)
		}
	}
	return parts
}

func validateCreatePathParts(rawPath string, parts []string, nodeType int16) error {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" || strings.HasPrefix(trimmed, "/") || strings.HasSuffix(trimmed, "/") || strings.Contains(trimmed, "//") {
		return ErrInvalid
	}
	for i, part := range parts {
		if domaincommon.ExceedNameLimit(part) {
			return ErrInvalid
		}
		if nodeType == 1 || i < len(parts)-1 {
			if _, exists := reservedCreateFolderNames[strings.ToLower(part)]; exists {
				return ErrInvalid
			}
		}
	}
	if nodeType == 1 {
		for _, part := range parts[:len(parts)-1] {
			if systemTopicTypeByName(part) != 0 {
				return ErrInvalid
			}
		}
		return nil
	}
	if len(parts) < 2 || systemTopicTypeByName(parts[len(parts)-2]) == 0 {
		return ErrInvalid
	}
	for _, part := range parts[:len(parts)-2] {
		if systemTopicTypeByName(part) != 0 {
			return ErrInvalid
		}
	}
	return nil
}

func lastNamespacePart(namespace string) string {
	parts := strings.Split(strings.Trim(namespace, "/"), "/")
	if len(parts) == 0 {
		return namespace
	}
	return parts[len(parts)-1]
}

func toInt64(value any) int64 {
	switch v := value.(type) {
	case int64:
		return v
	case int:
		return int64(v)
	case float64:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func (s *Service) commandToNode(ctx context.Context, cmd SaveCommand) (repo.UnsNode, error) {
	name := strings.TrimSpace(cmd.Name)
	displayName := strings.TrimSpace(cmd.DisplayName)
	if name == "" ||
		domaincommon.ExceedNameLimit(name) ||
		domaincommon.ExceedMaxLength(displayName, domaincommon.DisplayNameMaxLen) ||
		domaincommon.ExceedDescLimit(cmd.Description) {
		return repo.UnsNode{}, ErrInvalid
	}
	cmd.Namespace = cleanSaveNamespace(cmd)
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil {
		return repo.UnsNode{}, err
	}
	topicType, err := topicTypeValue(cmd.TopicType)
	if err != nil {
		return repo.UnsNode{}, err
	}
	if nodeType == 2 {
		namespaceTopicType := topicTypeFromNamespace(cmd.Namespace)
		if topicType == 0 {
			topicType = namespaceTopicType
		}
		if topicType == 0 {
			topicType = 1
		}
		if namespaceTopicType != 0 && namespaceTopicType != topicType {
			return repo.UnsNode{}, ErrInvalid
		}
	}
	if nodeType != 2 && cmd.DataQualityPolicy != nil {
		return repo.UnsNode{}, ErrInvalid
	}
	schema, err := jsonValue(cmd.Schema)
	if err != nil {
		return repo.UnsNode{}, err
	}
	schema, err = sanitizeNodeSchema(schema)
	if err != nil {
		return repo.UnsNode{}, err
	}
	extend, err := jsonValue(cmd.ExtendProperties)
	if err != nil {
		return repo.UnsNode{}, err
	}
	if nodeType == 1 {
		nameTopicType := topicTypeByPart(cmd.Name)
		if cmd.SystemTopicFolder && nameTopicType != 0 && (topicType == 0 || topicType == nameTopicType) {
			topicType = nameTopicType
		} else {
			topicType = 0
		}
	}
	alias, err := s.resolveNodeAlias(ctx, cmd, nodeType)
	if err != nil {
		return repo.UnsNode{}, err
	}
	return repo.UnsNode{
		ParentID:          cmd.ParentID,
		Name:              name,
		DisplayName:       displayName,
		Description:       cmd.Description,
		Alias:             alias,
		Namespace:         strings.Trim(cmd.Namespace, "/"),
		Type:              nodeType,
		TopicType:         topicType,
		Schema:            schema,
		ExtendProperties:  extend,
		DataQualityPolicy: normalizeFirstVersionDataQualityPolicy(cmd.DataQualityPolicy, schema),
		EnableHistory:     persistenceFlag(cmd.EnableHistory),
		MockData:          yesNoFlag(cmd.MockData),
	}, nil
}

func normalizeFirstVersionDataQualityPolicy(policy *repo.DataQualityPolicy, schema json.RawMessage) *repo.DataQualityPolicy {
	if policy == nil {
		return nil
	}
	normalized := repo.DataQualityPolicy{
		NotifyOnIssue:             false,
		ExpectedIntervalMs:        policy.ExpectedIntervalMs,
		MissingGraceMs:            policy.MissingGraceMs,
		FrequencyTolerancePercent: 20,
		MinSamples:                3,
		MandatoryFields:           []string{},
		FlatFields:                []string{},
		StaleFields:               []string{},
		SeverityOverrides:         map[string]string{},
		CooldownMs:                60000,
	}
	normalized.Rules = domaindataquality.FirstVersionRules(schema, normalized.ExpectedIntervalMs)
	normalized.Enabled = len(normalized.Rules) > 0
	if !normalized.Enabled && !normalized.NotifyOnIssue {
		return nil
	}
	return &normalized
}

func (s *Service) resolveNodeAlias(ctx context.Context, cmd SaveCommand, nodeType int16) (string, error) {
	alias := normalizeAliasCandidate(cmd.Alias)
	if alias != "" {
		if domaincommon.ExceedMaxLength(alias, domaincommon.AliasMaxLen) || !isAliasFormatOK(alias) {
			return "", fmt.Errorf("%w: invalid alias", ErrInvalid)
		}
		ok, err := s.aliasAvailable(ctx, alias, cmd.ID)
		if err != nil {
			return "", err
		}
		if !ok {
			return "", fmt.Errorf("alias already exists: alias=%s", alias)
		}
		return alias, nil
	}
	seed, err := s.aliasSeed(ctx, cmd)
	if err != nil {
		return "", err
	}
	for range 8 {
		alias := generateUnsAlias(seed, nodeType)
		ok, err := s.aliasAvailable(ctx, alias, cmd.ID)
		if err != nil {
			return "", err
		}
		if ok {
			return alias, nil
		}
	}
	return "", ErrInvalid
}

func (s *Service) aliasSeed(ctx context.Context, cmd SaveCommand) (string, error) {
	namespace := strings.Trim(cmd.Namespace, "/")
	if namespace != "" {
		return namespace, nil
	}
	name := strings.Trim(strings.TrimSpace(cmd.Name), "/")
	if cmd.ParentID == 0 {
		return name, nil
	}
	parent, err := s.uns.GetUnsNode(ctx, cmd.ParentID, false)
	if err != nil {
		return "", err
	}
	return strings.Trim(parent.Namespace+"/"+name, "/"), nil
}

func cleanSaveNamespace(cmd SaveCommand) string {
	namespace := strings.Trim(cmd.Namespace, "/")
	if cmd.ParentID != 0 {
		return ""
	}
	return namespace
}

func (s *Service) aliasAvailable(ctx context.Context, alias string, currentID int64) (bool, error) {
	if alias == "" {
		return false, nil
	}
	node, err := s.uns.GetUnsNodeByAlias(ctx, alias)
	if errors.Is(err, repo.ErrNotFound) {
		return true, nil
	}
	if err != nil {
		return false, err
	}
	return currentID != 0 && node.ID == currentID, nil
}

func (s *Service) templateCommandToRepo(cmd TemplateCommand) (repo.UnsTemplate, error) {
	name := strings.TrimSpace(cmd.Name)
	description := strings.TrimSpace(cmd.Description)
	if name == "" || domaincommon.ExceedNameLimit(name) || domaincommon.ExceedDescLimit(description) {
		return repo.UnsTemplate{}, ErrInvalid
	}
	for _, field := range cmd.Fields {
		if err := validateSchemaFieldName(field); err != nil {
			return repo.UnsTemplate{}, err
		}
	}
	topicType, err := topicTypeValue(cmd.TopicType)
	if err != nil {
		return repo.UnsTemplate{}, err
	}
	if topicType == 0 {
		topicType = 1
	}
	schema, err := json.Marshal(cmd.Fields)
	if err != nil {
		return repo.UnsTemplate{}, ErrInvalidJSON
	}
	extend, err := json.Marshal(map[string]any{"description": description})
	if err != nil {
		return repo.UnsTemplate{}, ErrInvalidJSON
	}
	return repo.UnsTemplate{
		Name:             name,
		TopicType:        topicType,
		Schema:           schema,
		ExtendProperties: extend,
	}, nil
}

func nodeResp(node repo.UnsNode) map[string]any {
	rawSchema := rawJSON(node.Schema)
	fields := schemaFieldsFromAny(rawSchema)
	dataType := dataTypeForNode(node, rawSchema)
	parentDataType := int(node.TopicType)
	persistence := node.EnableHistory == 1
	mockData := node.MockData == 1
	withFlow := mockData
	resp := map[string]any{
		"id":               node.ID,
		"idPath":           node.IDPath,
		"parentId":         node.ParentID,
		"name":             node.Name,
		"displayName":      node.DisplayName,
		"description":      node.Description,
		"namespace":        node.Namespace,
		"alias":            unsAlias(node),
		"type":             nodeTypeName(node.Type),
		"topicType":        topicTypeName(node.TopicType),
		"sortKey":          node.SortKey,
		"schema":           fields,
		"fields":           fields,
		"dataType":         dataType,
		"parentDataType":   parentDataType,
		"extendProperties": rawJSON(node.ExtendProperties),
		"status":           node.Status,
		"persistence":      persistence,
		"withFlow":         withFlow,
		"mockData":         mockData,
		"enableHistory":    node.EnableHistory,
		"isFavorite":       node.IsFavorite,
		"recycleIsDel":     node.RecycleIsDel,
		"hasChildren":      node.HasChildren,
		"countChildren":    node.CountChildren,
		"createdBy":        node.CreatedBy,
		"updatedBy":        node.UpdatedBy,
		"deletedBy":        node.DeletedBy,
		"createdTime":      node.CreatedTime,
		"updatedTime":      node.UpdatedTime,
		"deletedTime":      node.DeletedTime,
	}
	if node.Type == 2 && node.DataQualityPolicy != nil {
		resp["dataQualityPolicy"] = node.DataQualityPolicy
	}
	if dataType == 8 {
		resp["jsonFields"] = fields
	}
	return resp
}

func unsAlias(node repo.UnsNode) string {
	alias := normalizeAliasCandidate(node.Alias)
	if isAliasFormatOK(alias) {
		return alias
	}
	return stableAliasFromNode(node)
}

func stableAliasFromNode(node repo.UnsNode) string {
	seed := strings.Trim(node.Namespace, "/")
	if seed == "" {
		seed = strings.TrimSpace(node.Name)
	}
	if seed == "" {
		return ""
	}
	prefix := seed
	if node.Type == 2 {
		prefix = lastPathSegment(seed)
	} else if seg := lastPathSegment(seed); topicTypeByPart(seg) != 0 {
		prefix = strings.ToLower(seg)
	} else if strings.Count(seed, "/") > 1 {
		if start := lastOrdinalIndexOf(seed, "/", 2); start >= 0 {
			prefix = seed[start+1:]
		}
	}
	prefix = sanitizeAndPinyin(prefix)
	if prefix == "" {
		if node.Type == 2 {
			prefix = "file"
		} else {
			prefix = "folder"
		}
	}
	if runeLen(prefix) > aliasPrefixMaxRunes {
		prefix = firstNRunes(prefix, aliasPrefixMaxRunes)
	}
	if !startsWithAliasLetter(prefix) {
		prefix = "_" + prefix
	}
	return prefix + "_" + stableAliasSuffix(seed)
}

func generateUnsAlias(path string, nodeType int16) string {
	if nodeType == 2 {
		return generateFileAlias(path)
	}
	if seg := lastPathSegment(path); topicTypeByPart(seg) != 0 {
		return strings.ToLower(seg) + "_" + randomAliasSuffix()
	}
	aliasPath := path
	if strings.Count(path, "/") > 1 {
		if start := lastOrdinalIndexOf(path, "/", 2); start >= 0 {
			aliasPath = path[start:]
		}
	}
	aliasPath = sanitizeAndPinyin(aliasPath)
	if runeLen(aliasPath) > aliasPrefixMaxRunes {
		aliasPath = firstNRunes(aliasPath, aliasPrefixMaxRunes)
	}
	if aliasPath == "" {
		aliasPath = "folder"
	}
	aliasPath += "_" + randomAliasSuffix()
	if !startsWithAliasLetter(aliasPath) {
		aliasPath = "_" + aliasPath
	}
	return aliasPath
}

func generateFileAlias(path string) string {
	aliasSeed := lastPathSegment(path)
	if aliasSeed == "" {
		aliasSeed = strings.Trim(path, "/")
	}
	if aliasSeed == "" {
		aliasSeed = "file"
	}
	aliasPath := sanitizeAndPinyin(aliasSeed)
	if aliasPath == "" {
		aliasPath = "file"
	}
	if runeLen(aliasPath) > aliasPrefixMaxRunes {
		aliasPath = firstNRunes(aliasPath, aliasPrefixMaxRunes)
	}
	aliasPath += "_" + randomAliasSuffix()
	if !startsWithAliasLetter(aliasPath) {
		aliasPath = "_" + aliasPath
	}
	return aliasPath
}

func normalizeAliasCandidate(alias string) string {
	return strings.Trim(strings.TrimSpace(alias), "/")
}

func isAliasFormatOK(alias string) bool {
	if alias == "" {
		return false
	}
	for i, r := range alias {
		if i == 0 {
			if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' || r == '$') {
				return false
			}
			continue
		}
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '$') {
			return false
		}
	}
	return true
}

func sanitizeAndPinyin(value string) string {
	value = strings.ReplaceAll(value, "/", "_")
	value = strings.ReplaceAll(value, "-", "_")
	args := pinyin.NewArgs()
	args.Style = pinyin.Normal
	var b strings.Builder
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			for _, seg := range pinyin.LazyPinyin(string(r), args) {
				b.WriteString(seg)
			}
			continue
		}
		if isAliasPartRune(r) {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	return strings.Trim(b.String(), "_")
}

func isAliasPartRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '$'
}

func startsWithAliasLetter(value string) bool {
	if value == "" {
		return false
	}
	r, _ := utf8.DecodeRuneInString(value)
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func firstNRunes(value string, n int) string {
	if n <= 0 {
		return ""
	}
	i := 0
	for pos := range value {
		if i == n {
			return value[:pos]
		}
		i++
	}
	return value
}

func runeLen(value string) int {
	return utf8.RuneCountInString(value)
}

func lastPathSegment(path string) string {
	trimmed := strings.Trim(path, "/")
	if trimmed == "" {
		return ""
	}
	parts := strings.Split(trimmed, "/")
	return parts[len(parts)-1]
}

func lastOrdinalIndexOf(value, substr string, n int) int {
	if n <= 0 || substr == "" {
		return -1
	}
	positions := make([]int, 0, 8)
	start := 0
	for {
		pos := strings.Index(value[start:], substr)
		if pos == -1 {
			break
		}
		absolute := start + pos
		positions = append(positions, absolute)
		start = absolute + len(substr)
		if start >= len(value) {
			break
		}
	}
	if len(positions) < n {
		return -1
	}
	return positions[len(positions)-n]
}

func randomAliasSuffix() string {
	raw := strings.ReplaceAll(uuid.NewString(), "-", "")
	if len(raw) <= aliasSuffixHexLength {
		return raw
	}
	return raw[len(raw)-aliasSuffixHexLength:]
}

func stableAliasSuffix(seed string) string {
	sum := md5.Sum([]byte(seed))
	raw := hex.EncodeToString(sum[:])
	if len(raw) <= aliasSuffixHexLength {
		return raw
	}
	return raw[:aliasSuffixHexLength]
}

func schemaBool(schema any, keys ...string) bool {
	m, ok := schema.(map[string]any)
	if !ok {
		return false
	}
	for _, key := range keys {
		value, exists := m[key]
		if !exists {
			continue
		}
		switch v := value.(type) {
		case bool:
			return v
		case string:
			return strings.EqualFold(strings.TrimSpace(v), "true")
		case float64:
			return v != 0
		case int:
			return v != 0
		}
	}
	return false
}

func schemaFieldsFromRaw(raw json.RawMessage) []any {
	return schemaFieldsFromAny(rawJSON(raw))
}

func schemaFieldsFromAny(schema any) []any {
	switch value := schema.(type) {
	case []any:
		return normalizeSchemaFields(value)
	case map[string]any:
		if fields, ok := value["fields"].([]any); ok {
			return normalizeSchemaFields(fields)
		}
	}
	return []any{}
}

func normalizeSchemaFields(fields []any) []any {
	if len(fields) == 0 {
		return []any{}
	}
	out := make([]any, 0, len(fields))
	for _, field := range fields {
		if field == nil || isBlankSchemaField(field) {
			continue
		}
		out = append(out, field)
	}
	return out
}

func isBlankSchemaField(field any) bool {
	m, ok := field.(map[string]any)
	if !ok {
		return false
	}
	if len(m) == 0 {
		return true
	}
	for _, value := range m {
		if schemaValueHasContent(value) {
			return false
		}
	}
	return true
}

func schemaValueHasContent(value any) bool {
	switch v := value.(type) {
	case nil:
		return false
	case string:
		return strings.TrimSpace(v) != ""
	case bool:
		return v
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	case json.Number:
		return strings.TrimSpace(v.String()) != "" && v.String() != "0"
	case []any:
		for _, item := range v {
			if schemaValueHasContent(item) {
				return true
			}
		}
		return false
	case map[string]any:
		for _, item := range v {
			if schemaValueHasContent(item) {
				return true
			}
		}
		return false
	default:
		return true
	}
}

func dataTypeForNode(node repo.UnsNode, schema any) int {
	if m, ok := schema.(map[string]any); ok {
		if dataType := intFromAny(m["dataType"]); dataType > 0 {
			return dataType
		}
	}
	if node.Type != 2 {
		return int(node.TopicType)
	}
	switch node.TopicType {
	case 3:
		return 1
	case 1, 2:
		return 8
	default:
		return 0
	}
}

func intFromAny(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	case json.Number:
		n, _ := v.Int64()
		return int(n)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(v))
		return n
	default:
		return 0
	}
}

func sanitizeNodeSchema(schema json.RawMessage) (json.RawMessage, error) {
	var body any
	if len(schema) > 0 {
		if err := json.Unmarshal(schema, &body); err != nil {
			return nil, ErrInvalidJSON
		}
	}
	fields := schemaFieldsFromAny(body)
	for _, rawField := range fields {
		field, ok := rawField.(map[string]any)
		if !ok {
			continue
		}
		if err := validateSchemaFieldName(field); err != nil {
			return nil, err
		}
	}
	return json.Marshal(fields)
}

func validateSchemaFieldName(field map[string]any) error {
	rawName, ok := field["name"]
	if !ok {
		return nil
	}
	if domaincommon.ExceedNameLimit(strings.TrimSpace(fmt.Sprint(rawName))) {
		return ErrInvalid
	}
	return nil
}

func persistenceFlag(enabled *bool) int16 {
	if enabled != nil && *enabled {
		return 1
	}
	return 2
}

func yesNoFlag(enabled bool) int16 {
	if enabled {
		return 1
	}
	return 2
}

func isSystemFieldName(name string, topicType int16) bool {
	trimmed := strings.TrimSpace(name)
	lower := strings.ToLower(trimmed)
	switch lower {
	case "_id", "_timestamp", "_quality", "_ct", "_qos", "created_time":
		return true
	}
	if topicType != topicTypeMetricValue {
		return trimmed == "timeStamp" || lower == "timestamp" || lower == "quality"
	}
	return false
}

func templateResp(item repo.UnsTemplate) map[string]any {
	schema := rawJSON(item.Schema)
	extend := rawJSON(item.ExtendProperties)
	fields := schemaFieldsFromAny(schema)
	description := ""
	if m, ok := extend.(map[string]any); ok {
		if value, exists := m["description"]; exists {
			description = fmt.Sprint(value)
		}
	}
	return map[string]any{
		"id":               item.ID,
		"name":             item.Name,
		"topicType":        topicTypeName(item.TopicType),
		"fields":           fields,
		"description":      description,
		"extendProperties": extend,
		"createTime":       item.CreatedTime,
		"createdTime":      item.CreatedTime,
		"updatedTime":      item.UpdatedTime,
	}
}

func labelResp(item repo.UnsLabel) map[string]any {
	return map[string]any{
		"id":          item.ID,
		"labelKey":    item.LabelKey,
		"name":        item.Name,
		"color":       item.Color,
		"description": item.Description,
		"createTime":  item.CreatedTime,
		"createdTime": item.CreatedTime,
		"updatedTime": item.UpdatedTime,
	}
}

func nodesResp(nodes []repo.UnsNode) []map[string]any {
	out := make([]map[string]any, 0, len(nodes))
	for _, node := range nodes {
		out = append(out, nodeResp(node))
	}
	return out
}

func listResp(nodes []repo.UnsNode) map[string]any {
	return map[string]any{"list": nodesResp(nodes), "total": len(nodes)}
}

func nodeTypeValue(value string) (int16, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "folder", "path", "1":
		return 1, nil
	case "file", "topic", "2":
		return 2, nil
	default:
		return 0, ErrInvalid
	}
}

func nodeTypeName(value int16) string {
	if value == 2 {
		return "file"
	}
	return "folder"
}

func topicTypeValue(value string) (int16, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "0":
		return 0, nil
	case "state", "1":
		return 1, nil
	case "action", "2":
		return 2, nil
	case "metric", "3":
		return 3, nil
	default:
		return 0, ErrInvalid
	}
}

func topicTypeName(value int16) string {
	switch value {
	case 1:
		return "State"
	case 2:
		return "Action"
	case 3:
		return "Metric"
	default:
		return ""
	}
}

func topicTypeFromNamespace(namespace string) int16 {
	parts := strings.Split(strings.Trim(namespace, "/"), "/")
	if len(parts) < 2 {
		return 0
	}
	value, err := topicTypeValue(parts[len(parts)-2])
	if err != nil {
		return 0
	}
	return value
}

func topicTypeByPart(value string) int16 {
	topicType, err := topicTypeValue(value)
	if err != nil {
		return 0
	}
	return topicType
}

func jsonValue(value string) (json.RawMessage, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return json.RawMessage(`{}`), nil
	}
	if !json.Valid([]byte(value)) {
		return nil, ErrInvalidJSON
	}
	return json.RawMessage(value), nil
}

func rawJSON(value json.RawMessage) any {
	if len(value) == 0 {
		return map[string]any{}
	}
	var out any
	if err := json.Unmarshal(value, &out); err != nil {
		return string(value)
	}
	return out
}

func normalizeNotFound(err error) error {
	if errors.Is(err, repo.ErrNotFound) {
		return ErrNotFound
	}
	return err
}

var (
	ErrInvalid               = errors.New("invalid uns node")
	ErrInvalidJSON           = errors.New("invalid json")
	ErrInvalidImportFileType = errors.New("file format only supports .json")
	ErrInvalidImportFormat   = errors.New("invalid import file content")
	ErrNotFound              = errors.New("uns node not found")
	ErrRestoreConflict       = errors.New("uns restore conflict")
	ErrNotInRecycle          = errors.New("uns node is not in recycle")
)

func (e DeletedEvent) String() string {
	return fmt.Sprintf("deleted %d uns nodes", len(e.Nodes))
}
