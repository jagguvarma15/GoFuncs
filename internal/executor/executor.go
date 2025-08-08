package executor

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
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
	config Config
	nodeID string
	conn   *websocket.Conn
	sendCh chan *types.Message
	ctx    context.Context
	cancel context.CancelFunc
}

func New(config Config) (*Executor, error) {
	ctx, cancel := context.WithCancel(context.Background())

	nodeID := config.NodeID
	if nodeID == "" {
		nodeID = generateNodeID()
	}

	return &Executor{
		config: config,
		nodeID: nodeID,
		sendCh: make(chan *types.Message, 100),
		ctx:    ctx,
		cancel: cancel,
	}, nil
}

func (e *Executor) Start() error {
	if err := e.connectToCoordinator(); err != nil {
		return fmt.Errorf("failed to connect to coordinator: %w", err)
	}

	if e.config.Verbose {
		log.Printf("Executor %s started successfully", e.nodeID)
	}

	<-e.ctx.Done()
	return nil
}

func (e *Executor) Shutdown() error {
	if e.config.Verbose {
		log.Printf("Shutting down executor %s...", e.nodeID)
	}

	e.cancel()
	if e.conn != nil {
		e.conn.Close()
	}
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

	// Send a simple message to test connection
	if e.config.Verbose {
		log.Printf("Connected to coordinator successfully")
	}

	return nil
}

func generateNodeID() string {
	return fmt.Sprintf("executor-%d", time.Now().Unix())
}
