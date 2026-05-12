package tree_sitter_witcherscript_test

import (
	"testing"

	tree_sitter "github.com/smacker/go-tree-sitter"
	"github.com/webspam/tree-sitter-witcherscript"
)

func TestCanLoadGrammar(t *testing.T) {
	language := tree_sitter.NewLanguage(tree_sitter_witcherscript.Language())
	if language == nil {
		t.Errorf("Error loading Witcherscript grammar")
	}
}
