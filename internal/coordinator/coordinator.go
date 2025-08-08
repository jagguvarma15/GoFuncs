package coordinator

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"
	"github.com/jagguvarma15/GoFuncs/pkg/types"
)

type Config struct {
	Address    string
	Port       int
	ConfigPath string
	Verbose    bool
}

type Coordinator struct {
	config   Config
	server   *http.Server
	router   *mux.Router
	upgrader websocket.Upgrader

	// Cluster state
	cluster  *types.Cluster
	nodes    map[string]*NodeConnection
	nodesMux sync.RWMutex

	// Function registry
	functions map[string]*types.Function
	funcMux   sync.RWMutex

	// Request routing
	scheduler *Scheduler

	// Channels for coordination
	executionRequests chan *types.ExecutionRequest
	executionResults  chan *types.ExecutionResponse

	// Lifecycle
	ctx    context.Context
	cancel context.CancelFunc
}

type NodeConnection struct {
	Node   *types.Node
	Conn   *websocket.Conn
	SendCh chan *types.Message
}

func New(config Config) (*Coordinator, error) {
	ctx, cancel := context.WithCancel(context.Background())

	coord := &Coordinator{
		config:            config,
		upgrader:          websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		cluster:           &types.Cluster{Nodes: make(map[string]*types.Node), Functions: make(map[string]*types.Function)},
		nodes:             make(map[string]*NodeConnection),
		functions:         make(map[string]*types.Function),
		executionRequests: make(chan *types.ExecutionRequest, 1000),
		executionResults:  make(chan *types.ExecutionResponse, 1000),
		ctx:               ctx,
		cancel:            cancel,
	}

	coord.scheduler = NewScheduler(coord)
	coord.setupRoutes()

	return coord, nil
}

func (c *Coordinator) setupRoutes() {
	c.router = mux.NewRouter()

	// API routes
	api := c.router.PathPrefix("/api/v1").Subrouter()
	api.HandleFunc("/functions", c.handleListFunctions).Methods("GET")
	api.HandleFunc("/functions", c.handleDeployFunction).Methods("POST")
	api.HandleFunc("/functions/{name}", c.handleGetFunction).Methods("GET")
	api.HandleFunc("/functions/{name}", c.handleDeleteFunction).Methods("DELETE")
	api.HandleFunc("/functions/{name}/execute", c.handleExecuteFunction).Methods("POST")

	// Cluster management
	api.HandleFunc("/nodes", c.handleListNodes).Methods("GET")
	api.HandleFunc("/cluster/status", c.handleClusterStatus).Methods("GET")

	// WebSocket endpoint for node connections
	c.router.HandleFunc("/ws/node", c.handleNodeWebSocket)

	// Health check
	c.router.HandleFunc("/health", c.handleHealth).Methods("GET")

	// Static files for dashboard (future)
	c.router.PathPrefix("/").Handler(http.FileServer(http.Dir("./web/static/")))
}

func (c *Coordinator) Start() error {
	c.server = &http.Server{
		Addr:    fmt.Sprintf("%s:%d", c.config.Address, c.config.Port),
		Handler: c.router,
	}

	// Start background workers
	go c.processExecutionRequests()
	go c.processExecutionResults()
	go c.healthCheckNodes()

	if c.config.Verbose {
		log.Printf("Coordinator starting on %s", c.server.Addr)
	}

	return c.server.ListenAndServe()
}

func (c *Coordinator) Shutdown() error {
	if c.config.Verbose {
		log.Println("Shutting down coordinator...")
	}

	c.cancel()

	// Close all node connections
	c.nodesMux.Lock()
	for _, nodeConn := range c.nodes {
		nodeConn.Conn.Close()
	}
	c.nodesMux.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	return c.server.Shutdown(ctx)
}

// HTTP Handlers

func (c *Coordinator) handleListFunctions(w http.ResponseWriter, r *http.Request) {
	c.funcMux.RLock()
	functions := make([]*types.Function, 0, len(c.functions))
	for _, fn := range c.functions {
		functions = append(functions, fn)
	}
	c.funcMux.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"functions": functions,
		"count":     len(functions),
	})
}

func (c *Coordinator) handleDeployFunction(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Name     string            `json:"name"`
		Source   string            `json:"source"`
		Version  string            `json:"version"`
		Metadata map[string]string `json:"metadata"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	if req.Name == "" || req.Source == "" {
		http.Error(w, "Name and source are required", http.StatusBadRequest)
		return
	}

	if req.Version == "" {
		req.Version = "v1.0.0"
	}

	function := &types.Function{
		Name:      req.Name,
		Version:   req.Version,
		Source:    req.Source,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
		Metadata:  req.Metadata,
	}

	c.funcMux.Lock()
	c.functions[req.Name] = function
	c.cluster.Functions[req.Name] = function
	c.funcMux.Unlock()

	// Notify all nodes about the new function
	go c.distributeFunction(function)

	if c.config.Verbose {
		log.Printf("Function deployed: %s (version %s)", req.Name, req.Version)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(function)
}

func (c *Coordinator) handleGetFunction(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	c.funcMux.RLock()
	function, exists := c.functions[name]
	c.funcMux.RUnlock()

	if !exists {
		http.Error(w, "Function not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(function)
}

func (c *Coordinator) handleExecuteFunction(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	var req struct {
		Input   json.RawMessage `json:"input"`
		Timeout int             `json:"timeout,omitempty"`
	}

	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "Invalid JSON", http.StatusBadRequest)
		return
	}

	execReq := &types.ExecutionRequest{
		ID:           generateID(),
		FunctionName: name,
		Input:        req.Input,
		Timeout:      time.Duration(req.Timeout) * time.Second,
	}

	if execReq.Timeout == 0 {
		execReq.Timeout = 30 * time.Second
	}

	// Schedule execution
	result, err := c.scheduler.ScheduleExecution(execReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Execution failed: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(result)
}

func (c *Coordinator) handleListNodes(w http.ResponseWriter, r *http.Request) {
	c.nodesMux.RLock()
	nodes := make([]*types.Node, 0, len(c.cluster.Nodes))
	for _, node := range c.cluster.Nodes {
		nodes = append(nodes, node)
	}
	c.nodesMux.RUnlock()

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"nodes": nodes,
		"count": len(nodes),
	})
}

func (c *Coordinator) handleClusterStatus(w http.ResponseWriter, r *http.Request) {
	c.nodesMux.RLock()
	healthyNodes := 0
	totalCapacity := 0
	currentLoad := 0

	for _, node := range c.cluster.Nodes {
		if node.Status == types.NodeStatusHealthy {
			healthyNodes++
		}
		totalCapacity += node.Capacity.MaxConcurrentExecutions
		currentLoad += node.Capacity.CurrentExecutions
	}
	c.nodesMux.RUnlock()

	status := map[string]interface{}{
		"healthy_nodes":   healthyNodes,
		"total_nodes":     len(c.cluster.Nodes),
		"total_functions": len(c.functions),
		"total_capacity":  totalCapacity,
		"current_load":    currentLoad,
		"load_percentage": float64(currentLoad) / float64(totalCapacity) * 100,
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

func (c *Coordinator) handleHealth(w http.ResponseWriter, r *http.Request) {
	status := map[string]interface{}{
		"status":    "healthy",
		"timestamp": time.Now(),
		"version":   "v0.1.0",
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(status)
}

// WebSocket handler for node connections
func (c *Coordinator) handleNodeWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := c.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade failed: %v", err)
		return
	}

	go c.handleNodeConnection(conn)
}

// Background worker methods

func (c *Coordinator) processExecutionRequests() {
	for {
		select {
		case req := <-c.executionRequests:
			// Process execution request
			go c.handleExecutionRequest(req)
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Coordinator) processExecutionResults() {
	for {
		select {
		case result := <-c.executionResults:
			// Process execution result
			c.handleExecutionResult(result)
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Coordinator) healthCheckNodes() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			c.performHealthCheck()
		case <-c.ctx.Done():
			return
		}
	}
}

func (c *Coordinator) handleExecutionRequest(req *types.ExecutionRequest) {
	if c.config.Verbose {
		log.Printf("Processing execution request: %s for function %s", req.ID, req.FunctionName)
	}

	// Use scheduler to execute the function
	result, err := c.scheduler.ScheduleExecution(req)
	if err != nil {
		// Create error response
		errorResult := &types.ExecutionResponse{
			ID:        req.ID,
			Success:   false,
			Error:     err.Error(),
			Duration:  0,
			NodeID:    "coordinator",
			Timestamp: time.Now(),
		}
		c.executionResults <- errorResult
		return
	}

	c.executionResults <- result
}

func (c *Coordinator) handleExecutionResult(result *types.ExecutionResponse) {
	if c.config.Verbose {
		log.Printf("Execution result received: %s (success: %v, duration: %v)",
			result.ID, result.Success, result.Duration)
	}

	// Store result for client retrieval
	// In a production system, this would be stored in a cache or database
	// For now, just log it
}

func (c *Coordinator) performHealthCheck() {
	c.nodesMux.Lock()
	defer c.nodesMux.Unlock()

	now := time.Now()
	unhealthyNodes := []string{}

	for nodeID, node := range c.cluster.Nodes {
		// Mark nodes as unhealthy if they haven't been seen in 60 seconds
		if now.Sub(node.LastSeen) > 60*time.Second {
			if node.Status == types.NodeStatusHealthy {
				node.Status = types.NodeStatusUnhealthy
				unhealthyNodes = append(unhealthyNodes, nodeID)
			}
		}
	}

	if len(unhealthyNodes) > 0 && c.config.Verbose {
		log.Printf("Marked %d nodes as unhealthy: %v", len(unhealthyNodes), unhealthyNodes)
	}
}

func (c *Coordinator) distributeFunction(function *types.Function) {
	if c.config.Verbose {
		log.Printf("Distributing function %s to all nodes", function.Name)
	}

	// Create function deployment message
	message := &types.Message{
		Type:      types.MessageTypeFunctionDeploy,
		ID:        generateID(),
		Source:    "coordinator",
		Target:    "all",
		Payload:   c.marshalFunction(function),
		Timestamp: time.Now(),
	}

	// Send to all connected nodes
	c.nodesMux.RLock()
	for _, nodeConn := range c.nodes {
		select {
		case nodeConn.SendCh <- message:
			// Message sent successfully
		default:
			// Channel full, skip this node
			if c.config.Verbose {
				log.Printf("Failed to send function to node (channel full)")
			}
		}
	}
	c.nodesMux.RUnlock()
}

func (c *Coordinator) handleNodeConnection(conn *websocket.Conn) {
	defer conn.Close()

	// For now, this is a placeholder implementation
	// In a full implementation, this would:
	// 1. Handle node registration
	// 2. Process incoming messages from nodes
	// 3. Send outgoing messages to nodes
	// 4. Manage the connection lifecycle

	if c.config.Verbose {
		log.Printf("New node connection established from %s", conn.RemoteAddr())
	}

	// Simple message loop
	for {
		var message types.Message
		err := conn.ReadJSON(&message)
		if err != nil {
			if c.config.Verbose {
				log.Printf("Node connection closed: %v", err)
			}
			break
		}

		// Process the message
		c.handleNodeMessage(&message, conn)
	}
}

func (c *Coordinator) handleNodeMessage(message *types.Message, conn *websocket.Conn) {
	switch message.Type {
	case types.MessageTypeHeartbeat:
		// Handle heartbeat from node
		c.handleNodeHeartbeat(message, conn)
	case types.MessageTypeExecutionResponse:
		// Handle execution response from node
		c.handleNodeExecutionResponse(message)
	case types.MessageTypeNodeRegister:
		// Handle node registration
		c.handleNodeRegistration(message, conn)
	default:
		if c.config.Verbose {
			log.Printf("Unknown message type received: %v", message.Type)
		}
	}
}

func (c *Coordinator) handleNodeHeartbeat(message *types.Message, conn *websocket.Conn) {
	// Update node's last seen time
	c.nodesMux.Lock()
	if node, exists := c.cluster.Nodes[message.Source]; exists {
		node.LastSeen = time.Now()
		node.Status = types.NodeStatusHealthy
	}
	c.nodesMux.Unlock()
}

func (c *Coordinator) handleNodeExecutionResponse(message *types.Message) {
	// Unmarshal execution response and send to results channel
	// This is a simplified implementation
	result := &types.ExecutionResponse{
		ID:        message.ID,
		Success:   true,
		Output:    message.Payload,
		Duration:  time.Millisecond * 100, // Placeholder
		NodeID:    message.Source,
		Timestamp: time.Now(),
	}

	select {
	case c.executionResults <- result:
		// Result queued successfully
	default:
		// Results channel full
		if c.config.Verbose {
			log.Printf("Results channel full, dropping result")
		}
	}
}

func (c *Coordinator) handleNodeRegistration(message *types.Message, conn *websocket.Conn) {
	// Register a new node
	node := &types.Node{
		ID:       message.Source,
		Address:  conn.RemoteAddr().String(),
		Status:   types.NodeStatusHealthy,
		LastSeen: time.Now(),
		Capacity: types.NodeCapacity{
			MaxConcurrentExecutions: 10, // Default capacity
			CurrentExecutions:       0,
		},
		Functions: []string{},
		Metadata:  make(map[string]string),
	}

	c.nodesMux.Lock()
	c.cluster.Nodes[node.ID] = node
	c.nodes[node.ID] = &NodeConnection{
		Node:   node,
		Conn:   conn,
		SendCh: make(chan *types.Message, 100),
	}
	c.nodesMux.Unlock()

	if c.config.Verbose {
		log.Printf("Node registered: %s (%s)", node.ID, node.Address)
	}
}

func (c *Coordinator) handleDeleteFunction(w http.ResponseWriter, r *http.Request) {
	name := mux.Vars(r)["name"]

	c.funcMux.Lock()
	_, exists := c.functions[name]
	if !exists {
		c.funcMux.Unlock()
		http.Error(w, "Function not found", http.StatusNotFound)
		return
	}

	delete(c.functions, name)
	delete(c.cluster.Functions, name)
	c.funcMux.Unlock()

	if c.config.Verbose {
		log.Printf("Function deleted: %s", name)
	}

	// Notify all nodes to remove the function
	go c.distributeFunctionRemoval(name)

	w.WriteHeader(http.StatusNoContent)
}

func (c *Coordinator) distributeFunctionRemoval(functionName string) {
	if c.config.Verbose {
		log.Printf("Distributing function removal: %s", functionName)
	}

	message := &types.Message{
		Type:      types.MessageTypeFunctionRemove,
		ID:        generateID(),
		Source:    "coordinator",
		Target:    "all",
		Payload:   []byte(functionName),
		Timestamp: time.Now(),
	}

	c.nodesMux.RLock()
	for _, nodeConn := range c.nodes {
		select {
		case nodeConn.SendCh <- message:
			// Message sent successfully
		default:
			// Channel full, skip this node
		}
	}
	c.nodesMux.RUnlock()
}

func (c *Coordinator) marshalFunction(function *types.Function) []byte {
	// Simple JSON marshaling - in production, use proper JSON encoding
	return []byte(fmt.Sprintf(`{"name":"%s","version":"%s","source":"%s"}`,
		function.Name, function.Version, function.Source))
}

func generateID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}
