package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/jagguvarma15/GoFuncs/pkg/types"
)

type Config struct {
	Verbose bool
}

type FunctionRuntime struct {
	config    Config
	functions map[string]*types.Function
	funcMux   sync.RWMutex
}

func New(config Config) (*FunctionRuntime, error) {
	return &FunctionRuntime{
		config:    config,
		functions: make(map[string]*types.Function),
	}, nil
}

func (r *FunctionRuntime) DeployFunction(function *types.Function) error {
	r.funcMux.Lock()
	r.functions[function.Name] = function
	r.funcMux.Unlock()
	return nil
}

func (r *FunctionRuntime) RemoveFunction(functionName string) error {
	r.funcMux.Lock()
	delete(r.functions, functionName)
	r.funcMux.Unlock()
	return nil
}

func (r *FunctionRuntime) ExecuteFunction(ctx context.Context, functionName string, input []byte) ([]byte, error) {
	// Simple mock execution for testing
	result := map[string]interface{}{
		"message":   fmt.Sprintf("Function %s executed successfully", functionName),
		"input":     string(input),
		"timestamp": time.Now(),
	}

	return json.Marshal(result)
}
