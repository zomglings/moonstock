package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/bugout-dev/bugout-go/pkg/utils"
	"golang.org/x/time/rate"
)

var MainnetAPIURL string = "https://api.mainnet-beta.solana.com"
var CurrentJSONRPCVersion string = "2.0"

// Minimal implementation of a Solana JSON RPC API client. Only implements the methods we need
// to crawl information about metaplex marketplaces.
type SolanaClient struct {
	SolanaAPIURL string
	HTTPClient   *http.Client
	// For simplicity, we will use a single rate limiter to make sure we don't zergrush the
	// Solana JSON RPC API.
	// TODO(zomglings): I expect the limit on this rate limiter to be a bit less than the per-method
	// rate limit. If we are getting slowed down because we aren't making enough calls to different
	// methods at high enough concurrently, we should use this RateLimiter as a total rate limiter
	// (API-level) and also use separate method-level rate limiters with the lower method-specific
	// limits.
	RateLimiter       *rate.Limiter
	SolanaCoreVersion string
}

// Base struct representing a JSON RPC request to the Solana JSON RPC API.
// Parameters can be very different for different methods, so we use a generic []interface{}.
type SolanaJSONRPCRequest struct {
	JSONRPCVersion string        `json:"jsonrpc"`
	RequestID      int           `json:"id"`
	Method         string        `json:"method"`
	Parameters     []interface{} `json:"params,omitempty"`
}

// Base struct representing a JSON RPC resposne from the Solana JSON RPC API.
// Result type is dependent on method, so we use a generic interface{} to capture the result.
type SolanaJSONRPCResponse struct {
	JSONRPCVersion string      `json:"jsonrpc"`
	RequestID      int         `json:"id"`
	Result         interface{} `json:"result"`
}

type GetVersionResult struct {
	SolanaCoreVersion string `json:"solana-core"`
}

// TODO(zomglings): Add support for batched requests as per https://docs.solana.com/developing/clients/jsonrpc-api#request-formatting
// This is not a high priority for now because the response objects we are working with are quite large.
// When we are ready, start by defining:
// type SolanaJSONRPCRequestBatch []SolanaJSONRPCRequest

// Executes the Solana JSON RPC with the given methodName. Caller is expected to have validated
// that the parameters satisfy the schema expected by the Solana JSON RPC API.
// Also handles rate limiting as per SolanaClient configuration.
func (client *SolanaClient) Call(methodName string, parameters ...interface{}) (interface{}, error) {
	requestBody := SolanaJSONRPCRequest{
		JSONRPCVersion: CurrentJSONRPCVersion,
		// TODO(zomglings): Maybe we should vary the RequestID? Solana docs keep using 1, and we are
		// making synchronous API calls. This will become relevant only when we start using some asynchronous
		// HTTP client.
		RequestID: 1,
		Method:    methodName,
	}
	if len(parameters) > 0 {
		requestBody.Parameters = parameters
	}
	requestBuffer := new(bytes.Buffer)
	encodeErr := json.NewEncoder(requestBuffer).Encode(requestBody)
	if encodeErr != nil {
		return nil, encodeErr
	}

	request, requestErr := http.NewRequest("POST", client.SolanaAPIURL, requestBuffer)
	if requestErr != nil {
		return nil, requestErr
	}
	request.Header.Add("Content-Type", "application/json")
	request.Header.Add("Accept", "application/json")

	ctx := context.Background()
	// TODO(zomglings): Maybe we should add a parameter which allows users to set a timeout on
	// how long to wait here?
	rateLimitErr := client.RateLimiter.Wait(ctx)
	if rateLimitErr != nil {
		return nil, rateLimitErr
	}

	response, responseErr := client.HTTPClient.Do(request)
	if responseErr != nil {
		return nil, responseErr
	}
	defer response.Body.Close()

	statusErr := utils.HTTPStatusCheck(response)
	if statusErr != nil {
		return nil, statusErr
	}

	var APIResponse SolanaJSONRPCResponse
	decodeErr := json.NewDecoder(response.Body).Decode(&APIResponse)
	if decodeErr != nil {
		return nil, decodeErr
	}
	return APIResponse.Result, nil
}

func (client *SolanaClient) GetVersion() (GetVersionResult, error) {
	parsedResult := GetVersionResult{}
	result, err := client.Call("getVersion")
	if err != nil {
		return parsedResult, err
	}

	solanaCoreVersion, ok := result.(map[string]interface{})["solana-core"]
	if !ok {
		return parsedResult, fmt.Errorf("getVersion result did not contain solana-core key: %v", result)
	}
	parsedResult.SolanaCoreVersion = solanaCoreVersion.(string)
	return parsedResult, nil
}

// // https://docs.solana.com/developing/clients/jsonrpc-api#getblocks
// func (client *SolanaClient) GetBlocks(startSlot, endSlot uint64) {

// }

func NewSolanaClient(solanaAPIURL string, timeout time.Duration, requestRate float64) (*SolanaClient, error) {
	rateLimiter := rate.NewLimiter(rate.Limit(requestRate), int(requestRate))
	HTTPClient := http.Client{Timeout: timeout}
	client := SolanaClient{
		SolanaAPIURL: solanaAPIURL,
		HTTPClient:   &HTTPClient,
		RateLimiter:  rateLimiter,
	}

	result, err := client.GetVersion()
	if err != nil {
		return &client, fmt.Errorf("could not get Solana Core Version using getVersion call: %s", err.Error())
	}

	client.SolanaCoreVersion = result.SolanaCoreVersion

	return &client, nil
}
