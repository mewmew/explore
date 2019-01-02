package main

import (
	"os"
	"path/filepath"

	"github.com/llir/llvm/asm"
	"github.com/llir/llvm/ir"
	"github.com/mewkiz/pkg/jsonutil"
	"github.com/mewmew/lnp/pkg/cfa/primitive"
	"github.com/pkg/errors"
)

// parseModule parses the given LLVM IR assembly file into an LLVM IR module.
func parseModule(llPath string) (*ir.Module, error) {
	switch llPath {
	case "-":
		// Parse LLVM IR module from standard input.
		dbg.Printf("parsing standard input.")
		return asm.Parse("stdin", os.Stdin)
	default:
		dbg.Printf("parsing file %q.", llPath)
		return asm.ParseFile(llPath)
	}
}

// parsePrims parses the recovered control flow primitives of the given
// function.
func (e *explorer) parsePrims(funcName string) ([]*primitive.Primitive, error) {
	jsonName := funcName + ".json"
	jsonPath := filepath.Join(e.dotDir, jsonName)
	var prims []*primitive.Primitive
	if err := jsonutil.ParseFile(jsonPath, &prims); err != nil {
		return nil, errors.WithStack(err)
	}
	return prims, nil
}

// findFunc locates and returns the function with the specified name in the
// given module.
func findFunc(m *ir.Module, funcName string) (*ir.Func, error) {
	for _, f := range m.Funcs {
		if f.Name() == funcName {
			return f, nil
		}
	}
	return nil, errors.Errorf("unable to locate function %q in LLVM IR module", funcName)
}

// findBlock locates and returns the basic block with the specified name in the
// given function.
func findBlock(f *ir.Func, blockName string) (*ir.Block, error) {
	for _, block := range f.Blocks {
		if block.Name() == blockName {
			return block, nil
		}
	}
	return nil, errors.Errorf("unable to locate basic block %q in function %q", blockName, f.Name())
}
