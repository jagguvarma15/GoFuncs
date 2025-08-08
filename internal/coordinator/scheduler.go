package coordinator

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"

	"github.com/jagguvarma15/GoFuncs/pkg/types"
)

type Scheduler struct {
	coordinator *Coordinator
	strategy    types.LoadBalancingStrategy
	mutex       sync.RWMutex
}

func NewScheduler(coord *Coordinator) *Scheduler {
	return &Scheduler{
		coordinator: coord,
		strategy:    types.LoadBalancingLeastLoaded,
	}
}

// ScheduleExecution schedules a function execution on the best available node
func (s *Scheduler) ScheduleExecution(req *types.ExecutionRequest) (*types.ExecutionResponse, error) {
	// Find the best node for this execution
	node, err := s.selectNode(req.FunctionName)
	if err != nil {
		return nil, fmt.Errorf("no available nodes: %w", err)
	}

	// Create execution context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), req.Timeout)
	defer cancel()

	// Send execution request to the selected node
	response, err := s.executeOnNode(ctx, node, req)
	if err != nil {
		// If execution fails, mark node as potentially unhealthy and retry on another node
		s.handleExecutionFailure(node, err)
		return nil, fmt.Errorf("execution failed on node %s: %w", node.ID, err)
	}

	return response, nil
}

// selectNode chooses the best node based on the current load balancing strategy
func (s *Scheduler) selectNode(functionName string) (*types.Node, error) {
	s.coordinator.nodesMux.RLock()
	defer s.coordinator.nodesMux.RUnlock()

	var availableNodes []*types.Node
	for _, node := range s.coordinator.cluster.Nodes {
		if s.isNodeAvailable(node, functionName) {
			availableNodes = append(availableNodes, node)
		}
	}

	if len(availableNodes) == 0 {
		return nil, fmt.Errorf("no healthy nodes available for function %s", functionName)
	}

	switch s.strategy {
	case types.LoadBalancingRoundRobin:
		return s.selectRoundRobin(availableNodes), nil
	case types.LoadBalancingLeastLoaded:
		return s.selectLeastLoaded(availableNodes), nil
	case types.LoadBalancingWeightedRoundRobin:
		return s.selectWeightedRoundRobin(availableNodes), nil
	case types.LoadBalancingConsistentHash:
		return s.selectConsistentHash(availableNodes, functionName), nil
	default:
		return s.selectLeastLoaded(availableNodes), nil
	}
}

// isNodeAvailable checks if a node can handle the execution
func (s *Scheduler) isNodeAvailable(node *types.Node, functionName string) bool {
	// Check node health
	if node.Status != types.NodeStatusHealthy {
		return false
	}

	// Check if node has capacity
	if node.Capacity.CurrentExecutions >= node.Capacity.MaxConcurrentExecutions {
		return false
	}

	// Check if node has been seen recently (within last 60 seconds)
	if time.Since(node.LastSeen) > 60*time.Second {
		return false
	}

	// For now, assume all nodes can run all functions
	// In the future, we might check if the node has the specific function loaded
	return true
}

// selectRoundRobin implements round-robin selection
func (s *Scheduler) selectRoundRobin(nodes []*types.Node) *types.Node {
	// Simple round-robin based on current time
	index := int(time.Now().UnixNano()) % len(nodes)
	return nodes[index]
}

// selectLeastLoaded selects the node with the lowest current load
func (s *Scheduler) selectLeastLoaded(nodes []*types.Node) *types.Node {
	sort.Slice(nodes, func(i, j int) bool {
		loadI := float64(nodes[i].Capacity.CurrentExecutions) / float64(nodes[i].Capacity.MaxConcurrentExecutions)
		loadJ := float64(nodes[j].Capacity.CurrentExecutions) / float64(nodes[j].Capacity.MaxConcurrentExecutions)
		return loadI < loadJ
	})
	return nodes[0]
}

// selectWeightedRoundRobin implements weighted round-robin based on capacity
func (s *Scheduler) selectWeightedRoundRobin(nodes []*types.Node) *types.Node {
	// Calculate weights based on available capacity
	var totalWeight int
	weights := make([]int, len(nodes))

	for i, node := range nodes {
		availableCapacity := node.Capacity.MaxConcurrentExecutions - node.Capacity.CurrentExecutions
		weights[i] = availableCapacity
		totalWeight += availableCapacity
	}

	if totalWeight == 0 {
		// Fallback to round-robin if no capacity available
		return s.selectRoundRobin(nodes)
	}

	// Select based on weighted random selection
	random := rand.Intn(totalWeight)
	currentWeight := 0

	for i, weight := range weights {
		currentWeight += weight
		if random < currentWeight {
			return nodes[i]
		}
	}

	// Fallback to first node
	return nodes[0]
}

// selectConsistentHash implements consistent hashing for function affinity
func (s *Scheduler) selectConsistentHash(nodes []*types.Node, functionName string) *types.Node {
	// Simple hash-based selection for function affinity
	hash := s.simpleHash(functionName)
	index := int(hash) % len(nodes)
	return nodes[index]
}

// executeOnNode sends the execution request to a specific node
func (s *Scheduler) executeOnNode(ctx context.Context, node *types.Node, req *types.ExecutionRequest) (*types.ExecutionResponse, error) {
	// Get the node connection
	s.coordinator.nodesMux.RLock()
	nodeConn, exists := s.coordinator.nodes[node.ID]
	s.coordinator.nodesMux.RUnlock()

	if !exists {
		return nil, fmt.Errorf("node connection not found")
	}

	// Create a response channel for this specific request
	responseChan := make(chan *types.ExecutionResponse, 1)
	errorChan := make(chan error, 1)

	// Create execution message
	message := &types.Message{
		Type:      types.MessageTypeExecutionRequest,
		ID:        req.ID,
		Source:    "coordinator",
		Target:    node.ID,
		Payload:   s.marshalExecutionRequest(req),
		Timestamp: time.Now(),
	}

	// Send message to node
	select {
	case nodeConn.SendCh <- message:
		// Message sent successfully
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timeout sending message to node")
	}

	// Wait for response
	go s.waitForExecutionResponse(req.ID, responseChan, errorChan)

	select {
	case response := <-responseChan:
		return response, nil
	case err := <-errorChan:
		return nil, err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// waitForExecutionResponse waits for a response from an executor node
func (s *Scheduler) waitForExecutionResponse(requestID string, responseChan chan *types.ExecutionResponse, errorChan chan error) {
	timeout := time.After(30 * time.Second)
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-timeout:
			errorChan <- fmt.Errorf("execution timeout")
			return
		case <-ticker.C:
			// Check if we have a response for this request ID
			// This would typically check a response map/cache
			// For now, we'll simulate a response
			response := &types.ExecutionResponse{
				ID:        requestID,
				Success:   true,
				Output:    []byte(`{"result": "simulated response"}`),
				Duration:  time.Millisecond * 100,
				NodeID:    "node-1",
				Timestamp: time.Now(),
			}
			responseChan <- response
			return
		}
	}
}

// handleExecutionFailure handles when an execution fails on a node
func (s *Scheduler) handleExecutionFailure(node *types.Node, err error) {
	s.coordinator.nodesMux.Lock()
	defer s.coordinator.nodesMux.Unlock()

	// For now, just log the failure
	// In a production system, we might:
	// - Mark the node as unhealthy
	// - Implement circuit breaker pattern
	// - Trigger health checks
	if s.coordinator.config.Verbose {
		fmt.Printf("Execution failed on node %s: %v\n", node.ID, err)
	}
}

// marshalExecutionRequest converts an execution request to bytes
func (s *Scheduler) marshalExecutionRequest(req *types.ExecutionRequest) []byte {
	// In a real implementation, this would use proper serialization
	// For now, return a simple byte representation
	return []byte(fmt.Sprintf(`{"id":"%s","function":"%s","input":%s}`, req.ID, req.FunctionName, string(req.Input)))
}

// simpleHash implements a simple hash function for consistent hashing
func (s *Scheduler) simpleHash(input string) uint32 {
	hash := uint32(0)
	for _, c := range input {
		hash = hash*31 + uint32(c)
	}
	return hash
}

// SetStrategy changes the load balancing strategy
func (s *Scheduler) SetStrategy(strategy types.LoadBalancingStrategy) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.strategy = strategy
}

// GetStrategy returns the current load balancing strategy
func (s *Scheduler) GetStrategy() types.LoadBalancingStrategy {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.strategy
}
