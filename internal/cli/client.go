package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/jagguvarma15/GoFuncs/pkg/types"
)

type Config struct {
	CoordinatorAddress string
	Verbose            bool
}

type Client struct {
	config     Config
	httpClient *http.Client
	baseURL    string
}

func NewClient(config Config) *Client {
	return &Client{
		config: config,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
		baseURL: fmt.Sprintf("http://%s/api/v1", config.CoordinatorAddress),
	}
}

func (c *Client) DeployFunction(name, filePath, version string) error {
	if c.config.Verbose {
		fmt.Printf("Deploying function %s from %s (version %s)...\n", name, filePath, version)
	}

	// Read the function source code
	source, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read function file: %w", err)
	}

	// Create deployment request
	deployReq := map[string]interface{}{
		"name":    name,
		"source":  string(source),
		"version": version,
		"metadata": map[string]string{
			"file": filePath,
		},
	}

	// Send deployment request
	response, err := c.makeRequest("POST", "/functions", deployReq)
	if err != nil {
		return fmt.Errorf("deployment failed: %w", err)
	}

	var function types.Function
	if err := json.Unmarshal(response, &function); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	fmt.Printf("✅ Function '%s' deployed successfully\n", function.Name)
	fmt.Printf("   Version: %s\n", function.Version)
	fmt.Printf("   Created: %s\n", function.CreatedAt.Format(time.RFC3339))

	return nil
}

func (c *Client) CallFunction(name, inputJSON string) error {
	if c.config.Verbose {
		fmt.Printf("Executing function %s with input: %s\n", name, inputJSON)
	}

	// Parse input JSON
	var inputData interface{}
	if err := json.Unmarshal([]byte(inputJSON), &inputData); err != nil {
		return fmt.Errorf("invalid input JSON: %w", err)
	}

	// Create execution request
	execReq := map[string]interface{}{
		"input":   inputData,
		"timeout": 30,
	}

	startTime := time.Now()

	// Send execution request
	response, err := c.makeRequest("POST", fmt.Sprintf("/functions/%s/execute", name), execReq)
	if err != nil {
		return fmt.Errorf("execution failed: %w", err)
	}

	duration := time.Since(startTime)

	var execResponse types.ExecutionResponse
	if err := json.Unmarshal(response, &execResponse); err != nil {
		return fmt.Errorf("failed to parse execution response: %w", err)
	}

	// Display results
	fmt.Printf("🚀 Function '%s' executed successfully\n", name)
	fmt.Printf("   Duration: %v\n", duration)
	fmt.Printf("   Node: %s\n", execResponse.NodeID)
	fmt.Printf("   Success: %v\n", execResponse.Success)

	if execResponse.Success {
		fmt.Printf("   Result:\n")
		c.printFormattedJSON(execResponse.Output, 6)
	} else {
		fmt.Printf("   Error: %s\n", execResponse.Error)
	}

	return nil
}

func (c *Client) ListFunctions(format string) error {
	if c.config.Verbose {
		fmt.Println("Fetching function list...")
	}

	response, err := c.makeRequest("GET", "/functions", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch functions: %w", err)
	}

	var listResponse struct {
		Functions []*types.Function `json:"functions"`
		Count     int               `json:"count"`
	}

	if err := json.Unmarshal(response, &listResponse); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if format == "json" {
		return c.printJSON(listResponse)
	}

	// Table format
	if len(listResponse.Functions) == 0 {
		fmt.Println("No functions deployed.")
		return nil
	}

	fmt.Printf("📋 Found %d function(s):\n\n", listResponse.Count)

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tVERSION\tCREATED\tUPDATED")
	fmt.Fprintln(w, "----\t-------\t-------\t-------")

	for _, fn := range listResponse.Functions {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			fn.Name,
			fn.Version,
			fn.CreatedAt.Format("2006-01-02 15:04"),
			fn.UpdatedAt.Format("2006-01-02 15:04"),
		)
	}

	w.Flush()
	return nil
}

func (c *Client) RemoveFunction(name string) error {
	if c.config.Verbose {
		fmt.Printf("Removing function %s...\n", name)
	}

	_, err := c.makeRequest("DELETE", fmt.Sprintf("/functions/%s", name), nil)
	if err != nil {
		return fmt.Errorf("failed to remove function: %w", err)
	}

	fmt.Printf("🗑️  Function '%s' removed successfully\n", name)
	return nil
}

func (c *Client) ShowStatus(format string) error {
	if c.config.Verbose {
		fmt.Println("Fetching cluster status...")
	}

	response, err := c.makeRequest("GET", "/cluster/status", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch cluster status: %w", err)
	}

	var status map[string]interface{}
	if err := json.Unmarshal(response, &status); err != nil {
		return fmt.Errorf("failed to parse status response: %w", err)
	}

	if format == "json" {
		return c.printJSON(status)
	}

	// Table format
	fmt.Println("🏗️  Cluster Status:")
	fmt.Println()

	fmt.Printf("   Nodes:           %v healthy / %v total\n",
		status["healthy_nodes"], status["total_nodes"])
	fmt.Printf("   Functions:       %v deployed\n",
		status["total_functions"])
	fmt.Printf("   Capacity:        %v / %v executions\n",
		status["current_load"], status["total_capacity"])

	if loadPct, ok := status["load_percentage"].(float64); ok {
		fmt.Printf("   Load:            %.1f%%\n", loadPct)
	}

	return nil
}

func (c *Client) ListNodes(format string) error {
	if c.config.Verbose {
		fmt.Println("Fetching node list...")
	}

	response, err := c.makeRequest("GET", "/nodes", nil)
	if err != nil {
		return fmt.Errorf("failed to fetch nodes: %w", err)
	}

	var nodeResponse struct {
		Nodes []*types.Node `json:"nodes"`
		Count int           `json:"count"`
	}

	if err := json.Unmarshal(response, &nodeResponse); err != nil {
		return fmt.Errorf("failed to parse response: %w", err)
	}

	if format == "json" {
		return c.printJSON(nodeResponse)
	}

	// Table format
	if len(nodeResponse.Nodes) == 0 {
		fmt.Println("No executor nodes connected.")
		return nil
	}

	fmt.Printf("🖥️  Found %d executor node(s):\n\n", nodeResponse.Count)

	w := tabwriter.NewWriter(os.Stdout, 0, 8, 2, ' ', 0)
	fmt.Fprintln(w, "NODE ID\tSTATUS\tADDRESS\tLOAD\tLAST SEEN")
	fmt.Fprintln(w, "-------\t------\t-------\t----\t---------")

	for _, node := range nodeResponse.Nodes {
		loadStr := fmt.Sprintf("%d/%d",
			node.Capacity.CurrentExecutions,
			node.Capacity.MaxConcurrentExecutions)

		lastSeen := time.Since(node.LastSeen)
		lastSeenStr := formatDuration(lastSeen)

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			node.ID,
			string(node.Status),
			node.Address,
			loadStr,
			lastSeenStr,
		)
	}

	w.Flush()
	return nil
}

func (c *Client) makeRequest(method, path string, body interface{}) ([]byte, error) {
	url := c.baseURL + path

	var reqBody io.Reader
	if body != nil {
		jsonBody, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	if c.config.Verbose {
		fmt.Printf("Making %s request to %s\n", method, url)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("request failed with status %d: %s", resp.StatusCode, string(responseBody))
	}

	return responseBody, nil
}

func (c *Client) printJSON(data interface{}) error {
	jsonData, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(jsonData))
	return nil
}

func (c *Client) printFormattedJSON(data []byte, indent int) {
	var prettyJSON bytes.Buffer
	if err := json.Indent(&prettyJSON, data, "", strings.Repeat(" ", indent)); err != nil {
		// If formatting fails, just print the raw data
		fmt.Printf("%s%s\n", strings.Repeat(" ", indent), string(data))
		return
	}

	// Print each line with proper indentation
	lines := strings.Split(prettyJSON.String(), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) != "" {
			fmt.Printf("%s%s\n", strings.Repeat(" ", indent), line)
		}
	}
}

func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%.0fs ago", d.Seconds())
	} else if d < time.Hour {
		return fmt.Sprintf("%.0fm ago", d.Minutes())
	} else if d < 24*time.Hour {
		return fmt.Sprintf("%.0fh ago", d.Hours())
	} else {
		return fmt.Sprintf("%.0fd ago", d.Hours()/24)
	}
}
