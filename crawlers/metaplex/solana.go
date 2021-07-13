package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/bugout-dev/bugout-go/pkg/utils"
	"golang.org/x/mod/semver"
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
	RateLimiter         *rate.Limiter
	SolanaCoreVersion   string
	getBlocksMethodName string
	getBlockMethodName  string
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

// Result type for getVersion request
type GetVersionResult struct {
	SolanaCoreVersion string `json:"solana-core"`
}

// Result type for getBlocks request
type GetBlocksResult struct {
	BlockNumbers []uint64 `json:"blockNumbers"`
}

type ProgramInstruction struct {
	ProgramIDIndex int    `json:"programIdIndex"`
	Accounts       []int  `json:"accounts"`
	Data           string `json:"data"`
}

type TransactionHeader struct {
	NumRequiredSignatures       int `json:"numRequiredSignatures"`
	NumReadonlySignedAccounts   int `json:"numReadonlySignedAccounts"`
	NumReadonlyUnsignedAccounts int `json:"numReadonlyUnsignedAccounts"`
}

type TransactionMessage struct {
	AccountKeys     []string             `json:"accountKeys"`
	Header          TransactionHeader    `json:"header"`
	RecentBlockhash string               `json:"recentBlockhash"`
	Instructions    []ProgramInstruction `json:"instructions"`
}

type Transaction struct {
	Signatures []string           `json:"signatures"`
	Message    TransactionMessage `json:"message"`
}

type TransactionMetadata struct {
	Err          interface{} `json:"err"`
	Fee          uint64      `json:"fee"`
	PreBalances  []uint64    `json:"preBalances"`
	PostBalances []uint64    `json:"postBalances"`
	// TODO(zomglings): Add support for other metadata as per https://docs.solana.com/developing/clients/jsonrpc-api#results-2
}

type ResolvedTransaction struct {
	Transaction Transaction         `json:"transaction"`
	Meta        TransactionMetadata `json:"meta"`
}

type Reward struct {
	Pubkey      string `json:"pubkey"`
	Lamports    int64  `json:"lamports"`
	PostBalance uint64 `json:"postBalance"`
	RewardType  string `json:"rewardType"`
}

// Result type for getBlock request
type GetBlockResult struct {
	Blockhash         string                `json:"blockhash"`
	PreviousBlockhash string                `json:"previousBlockhash"`
	ParentSlot        uint64                `json:"parentSlot"`
	Transactions      []ResolvedTransaction `json:"transactions"`
	Signatures        []string              `json:"signatures"`
	Rewards           []Reward              `json:"rewards"`
	BlockTime         int64                 `json:"blockTime"`
	BlockHeight       uint64                `json:"blockHeight"`
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

// See the documentation at: https://docs.solana.com/developing/clients/jsonrpc-api#getversion
func (client *SolanaClient) GetVersion() (GetVersionResult, error) {
	parsedResult := GetVersionResult{}
	rawResult, err := client.Call("getVersion")
	if err != nil {
		return parsedResult, err
	}

	solanaCoreVersion, ok := rawResult.(map[string]interface{})["solana-core"]
	if !ok {
		return parsedResult, fmt.Errorf("getVersion result did not contain solana-core key: %v", rawResult)
	}
	parsedResult.SolanaCoreVersion = solanaCoreVersion.(string)
	return parsedResult, nil
}

// See the documentation at: https://docs.solana.com/developing/clients/jsonrpc-api#getblocks
func (client *SolanaClient) GetBlocks(startSlot, endSlot uint64) (GetBlocksResult, error) {
	parsedResult := GetBlocksResult{}
	// From documentation
	var maxSlotDifference uint64 = 500000
	if endSlot < startSlot {
		return parsedResult, fmt.Errorf("invalid parameters to getBlocks: startSlot(=%d) must not be greater than endSlot(=%d)", startSlot, endSlot)
	}
	if endSlot > startSlot+maxSlotDifference {
		return parsedResult, fmt.Errorf("invalid parameters to getBlocks: endSlot(=%d) must not be exceed startSlot(=%d) by more than %d", endSlot, startSlot, maxSlotDifference)
	}
	rawResult, err := client.Call(client.getBlocksMethodName, startSlot, endSlot)
	if err != nil {
		return parsedResult, fmt.Errorf("getBlocks call failed with error: %s", err.Error())
	}
	parsedResult.BlockNumbers = rawResult.([]uint64)
	return parsedResult, nil
}

// See the documentation at: https://docs.solana.com/developing/clients/jsonrpc-api#getblock
// TODO(zomglings): Add support for configuration object parameter. For now, the defaults give us
// all the information we need.
func (client *SolanaClient) GetBlock(slot uint64) (GetBlockResult, error) {
	parsedResult := GetBlockResult{}
	rawResult, err := client.Call(client.getBlockMethodName, slot)
	if err != nil {
		return parsedResult, fmt.Errorf("getBlock call failed with error: %s", err.Error())
	}
	if rawResult == nil {
		return parsedResult, errors.New("getBlock returned null result (could be because block is not confirmed)")
	}

	jsonBytes := new(bytes.Buffer)
	encodeErr := json.NewEncoder(jsonBytes).Encode(rawResult)
	if encodeErr != nil {
		return parsedResult, encodeErr
	}

	decodeErr := json.NewDecoder(jsonBytes).Decode(&parsedResult)

	return parsedResult, decodeErr
}

// Generates a new Solana client with the SolanaCoreVersion populated as well as the appropriate
// JSON RPC method names for methods that underwent deprecation and an upgrade with new names (e.g.
// getConfirmedBlock -> getBlock).
func NewSolanaClient(solanaAPIURL string, timeout time.Duration, requestRate float64) (*SolanaClient, error) {
	rateLimiter := rate.NewLimiter(rate.Limit(requestRate), int(requestRate))
	HTTPClient := http.Client{Timeout: timeout}
	client := SolanaClient{
		SolanaAPIURL:        solanaAPIURL,
		HTTPClient:          &HTTPClient,
		RateLimiter:         rateLimiter,
		getBlocksMethodName: "getBlocks",
		getBlockMethodName:  "getBlock",
	}

	result, err := client.GetVersion()
	if err != nil {
		return &client, fmt.Errorf("could not get Solana Core Version using getVersion call: %s", err.Error())
	}

	client.SolanaCoreVersion = result.SolanaCoreVersion

	if semver.Compare(fmt.Sprintf("v%s", client.SolanaCoreVersion), "v1.7.0") < 0 {
		client.getBlocksMethodName = "getConfirmedBlocks"
		client.getBlockMethodName = "getConfirmedBlock"
	}

	return &client, nil
}
