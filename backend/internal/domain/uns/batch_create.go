package uns

import (
	"bytes"
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"backend/internal/events"
	"backend/internal/infra/idgen"
	"backend/internal/repo"

	"gorm.io/gorm"
)

const unsCreateBatchAdvisoryKey = "tier0.uns.openapi.create.batch"

type CreateTreeNode struct {
	Command  SaveCommand
	Children []CreateTreeNode
}

type CreateTreeResult struct {
	Name      string
	Namespace string
	Err       error
}

type createTreeState struct {
	service          *Service
	ctx              context.Context
	byID             map[int64]repo.UnsNode
	childrenByParent map[int64]map[string]repo.UnsNode
	aliases          map[string]int64
	namespaces       map[string]int64
	created          []repo.UnsNode
	results          []CreateTreeResult
}

// CreateTreeBatch prepares an OpenAPI tree against one metadata snapshot and
// persists every valid node with batched INSERT statements in one transaction.
// Expected item failures stay isolated: descendants are skipped when their
// parent fails while later siblings continue to be prepared.
// Folder nodes that already exist (same parent, same case-insensitive name)
// are reused as parents instead of failing, so new topics can be appended
// under an existing branch; duplicate topic leaves still fail.
func (s *Service) CreateTreeBatch(ctx context.Context, roots []CreateTreeNode) ([]CreateTreeResult, error) {
	if len(roots) == 0 {
		return []CreateTreeResult{}, nil
	}
	db := repo.GetCommonConn(ctx)
	if db == nil {
		return nil, fmt.Errorf("database is not initialized")
	}

	state := &createTreeState{service: s, ctx: ctx}
	err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Exec(`SELECT pg_advisory_xact_lock(hashtext(?))`, unsCreateBatchAdvisoryKey).Error; err != nil {
			return err
		}
		txRepo := repo.NewUnsRepo(tx)
		existing, err := txRepo.ListActiveUnsNodesRaw(ctx)
		if err != nil {
			return err
		}
		state.initialize(existing)
		for _, root := range roots {
			state.walk(root, 0)
		}
		return txRepo.InsertUnsNodes(ctx, state.created)
	})
	if err != nil {
		return nil, err
	}
	if len(state.created) > 0 {
		nodes := append([]repo.UnsNode(nil), state.created...)
		s.publishPostCommit(ctx, events.UnsNodesCreated{Nodes: nodes})
	}
	return state.results, nil
}

func (s *createTreeState) initialize(existing []repo.UnsNode) {
	s.byID = make(map[int64]repo.UnsNode, len(existing))
	s.childrenByParent = make(map[int64]map[string]repo.UnsNode)
	s.aliases = make(map[string]int64, len(existing))
	s.namespaces = make(map[string]int64, len(existing))
	s.created = make([]repo.UnsNode, 0)
	s.results = make([]CreateTreeResult, 0)
	for _, node := range existing {
		s.indexNode(node)
	}
}

func (s *createTreeState) walk(input CreateTreeNode, parentID int64) {
	cmd := input.Command
	cmd.ParentID = parentID
	result := CreateTreeResult{Name: strings.TrimSpace(cmd.Name)}
	if existing, ok := s.reusableFolder(cmd); ok {
		result.Namespace = existing.Namespace
		s.results = append(s.results, result)
		for _, child := range input.Children {
			s.walk(child, existing.ID)
		}
		return
	}
	node, err := s.prepareExplicitNode(cmd)
	if err != nil {
		result.Err = err
		s.results = append(s.results, result)
		return
	}
	result.Namespace = node.Namespace
	s.results = append(s.results, result)
	if node.Type != 1 {
		return
	}
	for _, child := range input.Children {
		s.walk(child, node.ID)
	}
}

// reusableFolder returns the existing folder that a submitted folder node maps
// to: same parent, same case-insensitive name, and both nodes are folders.
// Topic leaves intentionally keep the strict duplicate semantics so callers
// can detect real conflicts on existing topics.
func (s *createTreeState) reusableFolder(cmd SaveCommand) (repo.UnsNode, bool) {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return repo.UnsNode{}, false
	}
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil || nodeType != 1 {
		return repo.UnsNode{}, false
	}
	existing, ok := s.childByName(cmd.ParentID, name)
	if !ok || existing.Type != 1 {
		return repo.UnsNode{}, false
	}
	return existing, true
}

func (s *createTreeState) prepareExplicitNode(cmd SaveCommand) (repo.UnsNode, error) {
	if strings.TrimSpace(cmd.Name) == "" {
		return repo.UnsNode{}, fmt.Errorf("name is required")
	}
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil {
		return repo.UnsNode{}, err
	}
	if nodeType == 2 {
		cmd, err = s.prepareFileParent(cmd)
		if err != nil {
			return repo.UnsNode{}, err
		}
	}
	return s.prepareNode(cmd)
}

func (s *createTreeState) prepareFileParent(cmd SaveCommand) (SaveCommand, error) {
	topicType, err := topicTypeValue(cmd.TopicType)
	if err != nil {
		return cmd, err
	}
	if topicType == 0 && cmd.ParentID != 0 {
		parent, ok := s.byID[cmd.ParentID]
		if !ok {
			return cmd, repo.ErrNotFound
		}
		if parent.TopicType != 0 {
			topicType = parent.TopicType
		}
	}
	if topicType == 0 {
		topicType = 1
	}
	cmd.TopicType = topicTypeName(topicType)

	if cmd.ParentID != 0 {
		parent, ok := s.byID[cmd.ParentID]
		if !ok {
			return cmd, repo.ErrNotFound
		}
		if parent.Type != 1 {
			return cmd, ErrInvalid
		}
		if parentNameTopicType := systemTopicTypeByName(parent.Name); parentNameTopicType != 0 {
			if parentNameTopicType != topicType {
				return cmd, ErrInvalid
			}
			return cmd, nil
		}
		if parent.TopicType == topicType {
			return cmd, nil
		}
		if parent.TopicType != 0 {
			return cmd, ErrInvalid
		}
	}

	folder, err := s.ensureSystemTopicFolder(cmd.ParentID, topicType, cmd.UserID)
	if err != nil {
		return cmd, err
	}
	cmd.ParentID = folder.ID
	return cmd, nil
}

func (s *createTreeState) ensureSystemTopicFolder(parentID int64, topicType int16, userID int64) (repo.UnsNode, error) {
	name := topicTypeName(topicType)
	if name == "" {
		return repo.UnsNode{}, ErrInvalid
	}
	if child, ok := s.childByName(parentID, name); ok {
		if child.Type != 1 || child.TopicType != topicType {
			return repo.UnsNode{}, ErrInvalid
		}
		return child, nil
	}
	return s.prepareNode(SaveCommand{
		ParentID:          parentID,
		Name:              name,
		DisplayName:       name,
		NodeType:          "folder",
		TopicType:         name,
		UserID:            userID,
		SystemTopicFolder: true,
	})
}

func (s *createTreeState) prepareNode(cmd SaveCommand) (repo.UnsNode, error) {
	name := strings.TrimSpace(cmd.Name)
	if name == "" {
		return repo.UnsNode{}, fmt.Errorf("name is required")
	}
	namespace, err := s.nodeNamespace(cmd.ParentID, name, cmd.Namespace)
	if err != nil {
		return repo.UnsNode{}, err
	}
	node, err := s.commandToNode(cmd, namespace)
	if err != nil {
		return repo.UnsNode{}, err
	}
	if err := s.service.publishInTx(s.ctx, events.UnsNodeSaving{Action: "create", Node: node, UserID: cmd.UserID}); err != nil {
		return repo.UnsNode{}, err
	}
	if err := s.validateParent(node); err != nil {
		return repo.UnsNode{}, err
	}
	if _, exists := s.childByName(node.ParentID, node.Name); exists {
		return repo.UnsNode{}, repo.ErrDuplicate
	}
	if _, exists := s.namespaces[node.Namespace]; exists {
		return repo.UnsNode{}, repo.ErrDuplicate
	}

	node.ID = idgen.NextID()
	node.IDPath, err = s.nodeIDPath(node.ParentID, node.ID)
	if err != nil {
		return repo.UnsNode{}, err
	}
	if err := validateBatchNodeStorage(node); err != nil {
		return repo.UnsNode{}, err
	}
	node.Status = "active"
	node.CreatedBy = cmd.UserID
	node.UpdatedBy = cmd.UserID
	s.created = append(s.created, node)
	s.indexNode(node)
	return node, nil
}

func (s *createTreeState) commandToNode(cmd SaveCommand, namespace string, excludeIDs ...int64) (repo.UnsNode, error) {
	nodeType, err := nodeTypeValue(cmd.NodeType)
	if err != nil {
		return repo.UnsNode{}, err
	}
	topicType, err := topicTypeValue(cmd.TopicType)
	if err != nil {
		return repo.UnsNode{}, err
	}
	if nodeType == 2 && topicType == 0 {
		topicType = 1
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
	alias, err := s.resolveAlias(cmd.Alias, namespace, nodeType, excludeIDs...)
	if err != nil {
		return repo.UnsNode{}, err
	}
	return repo.UnsNode{
		ParentID:          cmd.ParentID,
		Name:              strings.TrimSpace(cmd.Name),
		DisplayName:       strings.TrimSpace(cmd.DisplayName),
		Description:       cmd.Description,
		Alias:             alias,
		Namespace:         namespace,
		Type:              nodeType,
		TopicType:         topicType,
		Schema:            schema,
		ExtendProperties:  extend,
		DataQualityPolicy: cmd.DataQualityPolicy,
		EnableHistory:     persistenceFlag(cmd.EnableHistory),
		MockData:          yesNoFlag(cmd.MockData),
	}, nil
}

func (s *createTreeState) resolveAlias(rawAlias, namespace string, nodeType int16, excludeIDs ...int64) (string, error) {
	alias := normalizeAliasCandidate(rawAlias)
	if alias != "" {
		if !isAliasFormatOK(alias) {
			return "", fmt.Errorf("invalid alias: alias=%s", alias)
		}
		if id, exists := s.aliases[alias]; exists && (len(excludeIDs) == 0 || id != excludeIDs[0]) {
			return "", fmt.Errorf("alias already exists: alias=%s", alias)
		}
		return alias, nil
	}
	for range 8 {
		alias = generateUnsAlias(namespace, nodeType)
		if _, exists := s.aliases[alias]; !exists {
			return alias, nil
		}
	}
	return "", ErrInvalid
}

func (s *createTreeState) validateParent(node repo.UnsNode) error {
	if node.ParentID == 0 {
		return nil
	}
	parent, ok := s.byID[node.ParentID]
	if !ok {
		return repo.ErrNotFound
	}
	if parent.Type != 1 {
		return ErrInvalid
	}
	if parent.TopicType == 0 {
		return nil
	}
	if node.Type == 1 || node.TopicType == 0 || node.TopicType != parent.TopicType {
		return ErrInvalid
	}
	return nil
}

func (s *createTreeState) nodeNamespace(parentID int64, name, requested string) (string, error) {
	if parentID == 0 {
		if namespace := strings.Trim(requested, "/"); namespace != "" {
			return namespace, nil
		}
	}
	name = strings.Trim(strings.TrimSpace(name), "/")
	if name == "" {
		return "", repo.ErrInvalidUnsNode
	}
	if parentID == 0 {
		return name, nil
	}
	parent, ok := s.byID[parentID]
	if !ok {
		return "", repo.ErrNotFound
	}
	return strings.Trim(parent.Namespace+"/"+name, "/"), nil
}

func (s *createTreeState) nodeIDPath(parentID, id int64) (string, error) {
	if parentID == 0 {
		return strconv.FormatInt(id, 10), nil
	}
	parentPath, err := s.existingIDPath(parentID, map[int64]struct{}{})
	if err != nil {
		return "", err
	}
	return strings.Trim(parentPath, "/") + "/" + strconv.FormatInt(id, 10), nil
}

func (s *createTreeState) existingIDPath(id int64, visiting map[int64]struct{}) (string, error) {
	node, ok := s.byID[id]
	if !ok {
		return "", repo.ErrNotFound
	}
	if path := strings.Trim(node.IDPath, "/"); path != "" {
		return path, nil
	}
	if _, exists := visiting[id]; exists {
		return "", ErrInvalid
	}
	visiting[id] = struct{}{}
	defer delete(visiting, id)
	if node.ParentID == 0 {
		return strconv.FormatInt(node.ID, 10), nil
	}
	parentPath, err := s.existingIDPath(node.ParentID, visiting)
	if err != nil {
		return "", err
	}
	return parentPath + "/" + strconv.FormatInt(node.ID, 10), nil
}

func (s *createTreeState) childByName(parentID int64, name string) (repo.UnsNode, bool) {
	children := s.childrenByParent[parentID]
	if children == nil {
		return repo.UnsNode{}, false
	}
	node, ok := children[strings.ToLower(strings.TrimSpace(name))]
	return node, ok
}

func (s *createTreeState) indexNode(node repo.UnsNode) {
	s.byID[node.ID] = node
	children := s.childrenByParent[node.ParentID]
	if children == nil {
		children = make(map[string]repo.UnsNode)
		s.childrenByParent[node.ParentID] = children
	}
	key := strings.ToLower(strings.TrimSpace(node.Name))
	if _, exists := children[key]; !exists {
		children[key] = node
	}
	if node.Alias != "" {
		s.aliases[node.Alias] = node.ID
	}
	if node.Namespace != "" {
		s.namespaces[node.Namespace] = node.ID
	}
}

func validateBatchNodeStorage(node repo.UnsNode) error {
	if utf8.RuneCountInString(node.Name) > 128 ||
		utf8.RuneCountInString(node.DisplayName) > 256 ||
		utf8.RuneCountInString(node.Namespace) > 512 ||
		utf8.RuneCountInString(node.Alias) > 128 ||
		utf8.RuneCountInString(node.IDPath) > 1024 ||
		strings.ContainsRune(node.Name, '\x00') ||
		strings.ContainsRune(node.DisplayName, '\x00') ||
		strings.ContainsRune(node.Description, '\x00') ||
		strings.ContainsRune(node.Namespace, '\x00') ||
		bytes.Contains(node.Schema, []byte(`\u0000`)) ||
		bytes.Contains(node.ExtendProperties, []byte(`\u0000`)) {
		return ErrInvalid
	}
	return nil
}
