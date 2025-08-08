package types

import (
	"context"
	"time"
)

// Function represents a deployed function in the system
type Function struct {
	Name        string            `json:"name"`
	Version     string            `json:"version"`
	Source      string            `json:"source"`
	Signature   FunctionSignature `json:"signature"`
	CreatedAt   time.Time         `json:"created_at"`
	UpdatedAt   time.Time         `json:"updated_at"`
	Metadata    map[string]string `json:"metadata"`
}

// FunctionSignature describes the function's input and output types
type FunctionSignature struct {
	InputType  string `json:"input_type"`
	OutputType string `json:"output_type"`
	HasContext bool   `json:"has_context"`
	HasError   bool   `json:"has_error"`
}

// ExecutionRequest represents a request to execute a function
type ExecutionRequest struct {
	ID           string                 `json:"id"`
	FunctionName string                 `json:"function_name"`
	Input        []byte                 `json:"input"`
	Context      map[string]interface{} `json:"context,omitempty"`
	Timeout      time.Duration          `json:"timeout,omitempty"`
}

// ExecutionResponse represents the response from function execution
type ExecutionResponse struct {
	ID        string    `json:"id"`
	Success   bool      `json:"success"`
	Output    []byte    `json:"output,omitempty"`
	Error     string    `json:"error,omitempty"`
	Duration  time.Duration `json:"duration"`
	NodeID    string    `json:"node_id"`
	Timestamp time.Time `json:"timestamp"`
}

// Node represents an executor node in the cluster
type Node struct {
	ID           string            `json:"id"`
	Address      string            `json:"address"`
	Status       NodeStatus        `json:"status"`
	Capacity     NodeCapacity      `json:"capacity"`
	Functions    []string          `json:"functions"`
	LastSeen     time.Time         `json:"last_seen"`
	Metadata     map[string]string `json:"metadata"`
}

// NodeStatus represents the current status of a node
type NodeStatus string

const (
	NodeStatusHealthy     NodeStatus = "healthy"
	NodeStatusUnhealthy   NodeStatus = "unhealthy"
	NodeStatusMaintenance NodeStatus = "maintenance"
	NodeStatusDraining    NodeStatus = "draining"
)

// NodeCapacity represents the capacity and current load of a node
type NodeCapacity struct {
	MaxConcurrentExecutions int     `json:"max_concurrent_executions"`
	CurrentExecutions       int     `json:"current_executions"`
	CPUUsage               float64 `json:"cpu_usage"`
	MemoryUsage            float64 `json:"memory_usage"`
}

// Cluster represents the overall cluster state
type Cluster struct {
	Nodes     map[string]*Node `json:"nodes"`
	Functions map[string]*Function `json:"functions"`
}

// Message types for inter-node communication
type MessageType int

const (
	MessageTypeHeartbeat MessageType = iota
	MessageTypeExecutionRequest
	MessageTypeExecutionResponse
	MessageTypeFunctionDeploy
	MessageTypeFunctionRemove
	MessageTypeNodeRegister
	MessageTypeNodeUnregister
)

// Message represents communication between nodes
type Message struct {
	Type      MessageType `json:"type"`
	ID        string      `json:"id"`
	Source    string      `json:"source"`
	Target    string      `json:"target"`
	Payload   []byte      `json:"payload"`
	Timestamp time.Time   `json:"timestamp"`
}

// ExecutorFunction represents a function that can be executed
type ExecutorFunction interface {
	Execute(ctx context.Context, input []byte) ([]byte, error)
	GetSignature() FunctionSignature
	GetName() string
}

// LoadBalancingStrategy defines how requests are distributed
type LoadBalancingStrategy int

const (
	LoadBalancingRoundRobin LoadBalancingStrategy = iota
	LoadBalancingLeastLoaded
	LoadBalancingWeightedRoundRobin
	LoadBalancingConsistentHash
)

// Config represents the configuration for different components
type Config struct {
	Coordinator CoordinatorConfig `yaml:"coordinator"`
	Executor    ExecutorConfig    `yaml:"executor"`
	Cluster     ClusterConfig     `yaml:"cluster"`
}

type CoordinatorConfig struct {
	Address         string                `yaml:"address"`
	Port            int                   `yaml:"port"`
	LoadBalancing   LoadBalancingStrategy `yaml:"load_balancing"`
	HealthCheckInterval time.Duration     `yaml:"health_check_interval"`
}

type ExecutorConfig struct {
	CoordinatorAddress      string        `yaml:"coordinator_address"`
	MaxConcurrentExecutions int           `yaml:"max_concurrent_executions"`
	HeartbeatInterval       time.Duration `yaml:"heartbeat_interval"`
	FunctionTimeout         time.Duration `yaml:"function_timeout"`
}

type ClusterConfig struct {
	DiscoveryMethod string `yaml:"discovery_method"`
	ReplicationFactor int  `yaml:"replication_factor"`
}