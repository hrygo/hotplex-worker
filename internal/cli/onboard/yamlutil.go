package onboard

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

// lookupKey returns the value node for key in a MappingNode, or nil.
func lookupKey(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// lookupPath traverses nested mapping keys (e.g., "messaging", "slack").
// Transparently drills through a DocumentNode wrapper.
func lookupPath(root *yaml.Node, path ...string) *yaml.Node {
	cur := root
	if cur.Kind == yaml.DocumentNode && len(cur.Content) > 0 {
		cur = cur.Content[0]
	}
	for _, k := range path {
		cur = lookupKey(cur, k)
		if cur == nil {
			return nil
		}
	}
	return cur
}

// setScalar sets a scalar value for key in a MappingNode.
// Adds the key if it does not exist.
func setScalar(mapping *yaml.Node, key, value string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	if v := lookupKey(mapping, key); v != nil {
		v.Value = value
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: value},
	)
}

// setBool sets a boolean value for key in a MappingNode.
func setBool(mapping *yaml.Node, key string, value bool) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	if v := lookupKey(mapping, key); v != nil {
		v.Value = fmt.Sprintf("%t", value)
		v.Tag = "!!bool"
		return
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: fmt.Sprintf("%t", value)},
	)
}

// setStringList replaces (or adds) a sequence of strings for key.
// If values is empty the key is removed entirely.
func setStringList(mapping *yaml.Node, key string, values []string) {
	if mapping == nil || mapping.Kind != yaml.MappingNode {
		return
	}
	// Remove existing key.
	for i := 0; i+1 < len(mapping.Content); i += 2 {
		if mapping.Content[i].Value == key {
			mapping.Content = append(mapping.Content[:i], mapping.Content[i+2:]...)
			break
		}
	}
	if len(values) == 0 {
		return
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	for _, v := range values {
		seq.Content = append(seq.Content, &yaml.Node{
			Kind: yaml.ScalarNode, Tag: "!!str", Value: v,
		})
	}
	mapping.Content = append(mapping.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: key},
		seq,
	)
}

// replaceBlock deep-copies a mapping subtree from src into dst at the given path.
// Path elements are nested mapping keys, e.g. ("messaging", "slack").
// If the key does not exist in dst, it is appended.
func replaceBlock(dst, src *yaml.Node, path ...string) {
	srcBlock := lookupPath(src, path...)
	if srcBlock == nil {
		return
	}
	parent := lookupPath(dst, path[:len(path)-1]...)
	if parent == nil {
		return
	}
	key := path[len(path)-1]
	copied := deepCopyNode(srcBlock)
	for i := 0; i+1 < len(parent.Content); i += 2 {
		if parent.Content[i].Value == key {
			parent.Content[i+1] = copied
			return
		}
	}
	// Key not found in destination — append it.
	parent.Content = append(parent.Content,
		&yaml.Node{Kind: yaml.ScalarNode, Value: key},
		copied,
	)
}

func deepCopyNode(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	cp := *n
	if len(n.Content) > 0 {
		cp.Content = make([]*yaml.Node, len(n.Content))
		for i, child := range n.Content {
			cp.Content[i] = deepCopyNode(child)
		}
	}
	return &cp
}
