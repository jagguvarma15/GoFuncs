package executor

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
	"github.com/jagguvarma15/GoFuncs/internal/runtime"
	"github.com/jagguvarma15/GoFuncs/pkg/types"
)

type Config struct {
	CoordinatorAddress      string
	NodeID                  string
	MaxConcurrentExecutions int
	HeartbeatInterval       time.Duration
	Port                    int
	Verbose                 bool
}

type Executor struct {
	config  Config
	nodeID  string
	runtime *runtime.FunctionRuntime

	// Connection to coordinator
	conn   *websocket.Conn
	sendCh chan *types.Message

	// Function execution
	executions    map[string]*ExecutionContext
	executionsMux sync.RWMutex
	currentLoad   int
	loadMux       sync.RWMutex

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

type ExecutionContext struct {
	Request   *types.ExecutionRequest
	StartTime time.Time
	Cancel    context.CancelFunc
}

func New(config Config) (*Executor, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Generate node ID if not provided
	nodeID := config.NodeID
	if nodeID == "" {
		nodeID = generateNodeID()
	}

	// Create function runtime
	functionRuntime, err := runtime.New(runtime.Config{
		Verbose: config.Verbose,
	})
	if err != nil {
		cancel()
		return nil, fmt.Errorf("failed to create function runtime: %w", err)
	}

	executor := &Executor{
		config:     config,
		nodeID:     nodeID,
		runtime:    functionRuntime,
		sendCh:     make(chan *types.Message, 100),
		executions: make(map[string]*ExecutionContext),
		ctx:        ctx,
		cancel:     cancel,
	}

	return executor, nil
}

func (e *Executor) Start() error {
	// Connect to coordinator
	if err := e.connectToCoordinator(); err != nil {
		return fmt.Errorf("failed to connect to coordinator: %w", err)
	}

	// Start background workers
	go e.handleIncomingMessages()
	go e.handleOutgoingMessages()
	go e.sendHeartbeats()

	// Register with coordinator
	if err := e.registerWithCoordinator(); err != nil {
		return fmt.Errorf("failed to register with coordinator: %w", err)
	}

	if e.config.Verbose {
		log.Printf("Executor %s started successfully", e.nodeID)
	}

	// Keep running until context is cancelled
	<-e.ctx.Done()
	return nil
}

func (e *Executor) Shutdown() error {
	if e.config.Verbose {
		log.Printf("Shutting down executor %s...", e.nodeID)
	}

	e.cancel()

	// Close WebSocket connection
	if e.conn != nil {
		e.conn.Close()
	}

	// Wait for ongoing executions to complete (with timeout)
	e.waitForExecutions(30 * time.Second)

	return nil
}

func (e *Executor) GetNodeID() string {
	return e.nodeID
}

func (e *Executor) connectToCoordinator() error {
	coordinatorURL := url.URL{
		Scheme: "ws",
		Host:   e.config.CoordinatorAddress,
		Path:   "/ws/node",
	}

	if e.config.Verbose {
		log.Printf("Connecting to coordinator at %s", coordinatorURL.String())
	}

	conn, _, err := websocket.DefaultDialer.Dial(coordinatorURL.String(), nil)
	if err != nil {
		return fmt.Errorf("websocket dial failed: %w", err)
	}

	e.conn = conn
	return nil
}

func (e *Executor) registerWithCoordinator() error {
	registrationMessage := &types.Message{
		Type:      types.MessageTypeNodeRegister,
		ID:        generateMessageID(),
		Source:    e.nodeID,
		Target:    "coordinator",
		Payload:   e.marshalNodeInfo(),
		Timestamp: time.Now(),
	}

	select {
	case e.sendCh <- registrationMessage:
		if e.config.Verbose {
			log.Printf("Sent registration message to coordinator")
		}
		return nil
	case <-time.After(5 * time.Second):
		return fmt.Errorf("timeout sending registration message")
	}
}

func (e *Executor) handleIncomingMessages() {
	defer e.conn.Close()

	for {
		select {
		case <-e.ctx.Done():
			return
		default:
			var message types.Message
			err := e.conn.ReadJSON(&message)
			if err != nil {
				if e.config.Verbose {
					log.Printf("Error reading message: %v", err)
				}
				return
			}

			e.processMessage(&message)
		}
	}
}

func (e *Executor) handleOutgoingMessages() {
	for {
		select {
		case message := <-e.sendCh:
			err := e.conn.WriteJSON(message)
			if err != nil {
				if e.config.Verbose {
					log.Printf("Error sending message: %v", err)
				}
				return
			}
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *Executor) sendHeartbeats() {
	ticker := time.NewTicker(e.config.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			heartbeat := &types.Message{
				Type:      types.MessageTypeHeartbeat,
				ID:        generateMessageID(),
				Source:    e.nodeID,
				Target:    "coordinator",
				Payload:   e.marshalNodeStatus(),
				Timestamp: time.Now(),
			}

			select {
			case e.sendCh <- heartbeat:
				// Heartbeat sent
			default:
				// Send channel full, skip this heartbeat
			}
		case <-e.ctx.Done():
			return
		}
	}
}

func (e *Executor) processMessage(message *types.Message) {
	switch message.Type {
	case types.MessageTypeExecutionRequest:
		e.handleExecutionRequest(message)
	case types.MessageTypeFunctionDeploy:
		e.handleFunctionDeploy(message)
	case types.MessageTypeFunctionRemove:
		e.handleFunctionRemove(message)
	default:
		if e.config.Verbose {
			log.Printf("Unknown message type: %v", message.Type)
		}
	}
}

func (e *Executor) handleExecutionRequest(message *types.Message) {
	// Check if we have capacity
	e.loadMux.RLock()
	if e.currentLoad >= e.config.MaxConcurrentExecutions {
		e.loadMux.RUnlock()
		e.sendExecutionError(message.ID, "executor at capacity")
		return
	}
	e.loadMux.RUnlock()

	// Parse execution request
	request, err := e.unmarshalExecutionRequest(message.Payload)
	if err != nil {
		e.sendExecutionError(message.ID, fmt.Sprintf("invalid request: %v", err))
		return
	}

	// Execute function asynchronously
	go e.executeFunction(request)
}

func (e *Executor) executeFunction(request *types.ExecutionRequest) {
	startTime := time.Now()

	// Create execution context
	execCtx, cancel := context.WithTimeout(e.ctx, request.Timeout)
	defer cancel()

	execContext := &ExecutionContext{
		Request:   request,
		StartTime: startTime,
		Cancel:    cancel,
	}

	// Track execution
	e.executionsMux.Lock()
	e.executions[request.ID] = execContext
	e.executionsMux.Unlock()

	e.loadMux.Lock()
	e.currentLoad++
	e.loadMux.Unlock()

	defer func() {
		// Cleanup execution tracking
		e.executionsMux.Lock()
		delete(e.executions, request.ID)
		e.executionsMux.Unlock()

		e.loadMux.Lock()
		e.currentLoad--
		e.loadMux.Unlock()
	}()

	if e.config.Verbose {
		log.Printf("Executing function %s (request %s)", request.FunctionName, request.ID)
	}

	// Execute the function using the runtime
	result, err := e.runtime.ExecuteFunction(execCtx, request.FunctionName, request.Input)

	duration := time.Since(startTime)

	// Create response
	response := &types.ExecutionResponse{
		ID:        request.ID,
		Success:   err == nil,
		Duration:  duration,
		NodeID:    e.nodeID,
		Timestamp: time.Now(),
	}

	if err != nil {
		response.Error = err.Error()
	} else {
		response.Output = result
	}

	// Send response back to coordinator
	e.sendExecutionResponse(response)

	if e.config.Verbose {
		log.Printf("Function %s completed in %v (success: %v)",
			request.FunctionName, duration, response.Success)
	}
}

func (e *Executor) sendExecutionResponse(response *types.ExecutionResponse) {
	message := &types.Message{
		Type:      types.MessageTypeExecutionResponse,
		ID:        response.ID,
		Source:    e.nodeID,
		Target:    "coordinator",
		Payload:   e.marshalExecutionResponse(response),
		Timestamp: time.Now(),
	}

	select {
	case e.sendCh <- message:
		// Response sent
	case <-time.After(5 * time.Second):
		if e.config.Verbose {
			log.Printf("Timeout sending execution response")
		}
	}
}

func (e *Executor) sendExecutionError(requestID, errorMsg string) {
	response := &types.ExecutionResponse{
		ID:        requestID,
		Success:   false,
		Error:     errorMsg,
		Duration:  0,
		NodeID:    e.nodeID,
		Timestamp: time.Now(),
	}

	e.sendExecutionResponse(response)
}

func (e *Executor) handleFunctionDeploy(message *types.Message) {
	function, err := e.unmarshalFunction(message.Payload)
	if err != nil {
		if e.config.Verbose {
			log.Printf("Error unmarshaling function: %v", err)
		}
		return
	}

	if e.config.Verbose {
		log.Printf("Deploying function: %s", function.Name)
	}

	// Deploy function to runtime
	err = e.runtime.DeployFunction(function)
	if err != nil {
		if e.config.Verbose {
			log.Printf("Error deploying function %s: %v", function.Name, err)
		}
		return
	}

	if e.config.Verbose {
		log.Printf("Function %s deployed successfully", function.Name)
	}
}

func (e *Executor) handleFunctionRemove(message *types.Message) {
	functionName := string(message.Payload)

	if e.config.Verbose {
		log.Printf("Removing function: %s", functionName)
	}

	err := e.runtime.RemoveFunction(functionName)
	if err != nil {
		if e.config.Verbose {
			log.Printf("Error removing function %s: %v", functionName, err)
		}
		return
	}

	if e.config.Verbose {
		log.Printf("Function %s removed successfully", functionName)
	}
}

func (e *Executor) waitForExecutions(timeout time.Duration) {
	deadline := time.Now().Add(timeout)

	for time.Now().Before(deadline) {
		e.executionsMux.RLock()
		if len(e.executions) == 0 {
			e.executionsMux.RUnlock()
			return
		}
		e.executionsMux.RUnlock()

		time.Sleep(100 * time.Millisecond)
	}

	// Force cancel remaining executions
	e.executionsMux.Lock()
	for _, exec := range e.executions {
		exec.Cancel()
	}
	e.executionsMux.Unlock()
}

// Helper functions for marshaling/unmarshaling
func (e *Executor) marshalNodeInfo() []byte {
	return []byte(fmt.Sprintf(`{"id":"%s","max_executions":%d}`,
		e.nodeID, e.config.MaxConcurrentExecutions))
}

func (e *Executor) marshalNodeStatus() []byte {
	e.loadMux.RLock()
	load := e.currentLoad
	e.loadMux.RUnlock()

	return []byte(fmt.Sprintf(`{"current_executions":%d,"max_executions":%d}`,
		load, e.config.MaxConcurrentExecutions))
}

func (e *Executor) marshalExecutionResponse(response *types.ExecutionResponse) []byte {
	// Simplified marshaling - in production, use proper JSON
	if response.Success {
		return []byte(fmt.Sprintf(`{"id":"%s","success":true,"output":%s,"duration":"%v"}`,
			response.ID, string(response.Output), response.Duration))
	} else {
		return []byte(fmt.Sprintf(`{"id":"%s","success":false,"error":"%s","duration":"%v"}`,
			response.ID, response.Error, response.Duration))
	}
}

func (e *Executor) unmarshalExecutionRequest(payload []byte) (*types.ExecutionRequest, error) {
	// Simplified unmarshaling - in production, use proper JSON
	request := &types.ExecutionRequest{
		ID:           generateMessageID(),
		FunctionName: "Hello", // Placeholder
		Input:        payload,
		Timeout:      30 * time.Second,
	}
	return request, nil
}

func (e *Executor) unmarshalFunction(payload []byte) (*types.Function, error) {
	// Simplified unmarshaling - in production, use proper JSON
	function := &types.Function{
		Name:    "Hello",
		Source:  string(payload),
		Version: "v1.0.0",
	}
	return function, nil
}

func generateNodeID() string {
	return fmt.Sprintf("executor-%d", time.Now().Unix())
}

func generateMessageID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
