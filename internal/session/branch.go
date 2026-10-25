package session

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/elhenro/bee/internal/types"
)

// Fork creates a new session rollout containing all messages up to and including
// fromMsgID. If fromMsgID is empty the entire source session is copied.
// The new rollout is returned open; caller is responsible for Close.
func Fork(srcID, fromMsgID string) (*Rollout, error) {
	msgs, err := Read(srcID)
	if err != nil {
		return nil, err
	}
	keep := msgs
	if fromMsgID != "" {
		i := slices.IndexFunc(msgs, func(m types.Message) bool { return m.ID == fromMsgID })
		if i < 0 {
			return nil, fmt.Errorf("session: message %q not found in %s", fromMsgID, srcID)
		}
		keep = msgs[:i+1]
	}
	newID := uuid.NewString()
	r, err := Open(newID)
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	for _, m := range keep {
		if err := r.Append(ctx, m); err != nil {
			r.Close()
			return nil, err
		}
	}
	return r, nil
}

// BuildTree reconstructs the parent-pointer tree from a flat message slice.
// Returns the root node (first message with empty ParentID, or with a parent
// not present in the slice). If multiple roots exist, the first-seen wins and
// orphan subtrees are dropped — callers should ensure input is from one
// session. Returns nil if msgs is empty.
func BuildTree(msgs []types.Message) *types.MessageNode {
	if len(msgs) == 0 {
		return nil
	}
	nodes := make(map[string]*types.MessageNode, len(msgs))
	for i := range msgs {
		m := msgs[i]
		nodes[m.ID] = &types.MessageNode{Msg: m}
	}
	var root *types.MessageNode
	for i := range msgs {
		m := msgs[i]
		n := nodes[m.ID]
		if m.ParentID == "" {
			if root == nil {
				root = n
			}
			continue
		}
		p, ok := nodes[m.ParentID]
		if !ok {
			// parent not in set — treat as root candidate
			if root == nil {
				root = n
			}
			continue
		}
		p.Children = append(p.Children, n)
	}
	return root
}

// LinearPath walks from leafID up to root, returning the chain in
// root→leaf order. Returns nil if leafID isn't reachable.
func LinearPath(root *types.MessageNode, leafID string) []*types.MessageNode {
	if root == nil {
		return nil
	}
	// build id→node map and parent map via DFS
	parent := map[string]*types.MessageNode{}
	byID := map[string]*types.MessageNode{root.Msg.ID: root}
	var walk func(n *types.MessageNode)
	walk = func(n *types.MessageNode) {
		for _, c := range n.Children {
			parent[c.Msg.ID] = n
			byID[c.Msg.ID] = c
			walk(c)
		}
	}
	walk(root)

	leaf, ok := byID[leafID]
	if !ok {
		return nil
	}
	// climb to root, then flip into root→leaf order
	var path []*types.MessageNode
	for cur := leaf; cur != nil; cur = parent[cur.Msg.ID] {
		path = append(path, cur)
	}
	slices.Reverse(path)
	return path
}
