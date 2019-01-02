package main

import (
	"github.com/llir/llvm/ir"
	"github.com/llir/llvm/ir/metadata"
	"github.com/pkg/errors"
)

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

// diLocation returns the DILocation specialized metadata node based on the
// given MDNode. The boolean return value indicates success.
func diLocation(node metadata.MDNode) (*metadata.DILocation, bool) {
	if loc, ok := node.(*metadata.DILocation); ok {
		return loc, true
	}
	return nil, false
}
