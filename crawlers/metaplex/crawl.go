package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"

	bugout "github.com/bugout-dev/bugout-go/pkg"
	spire "github.com/bugout-dev/bugout-go/pkg/spire"
)

var SolanaMainnetAPIURL = "https://api.mainnet-beta.solana.com"

// Taken from: https://github.com/metaplex-foundation/metaplex/blob/7b76a0b99348cd5062274095ca904dbe22359d6e/js/packages/common/src/utils/ids.ts
const MetaplexAccountKey = "p1exdMJcjVao65QdewkaZRUnU6VPSXhus9n2GzWfh98"

// Coordinates a crawl of the Solana blockchain looking for information about Metaplex
type MetaplexCrawler struct {
	SolanaClient       *SolanaClient
	BugoutClient       *bugout.BugoutClient
	BugoutToken        string
	BugoutJournalID    string
	CursorName         string
	NextSlot           uint64
	MetaplexAccountKey string
}

// Gets the value of a cursor from the given journal. The value is assumed to be stored in the
// ContextUrl field of the journal entry representing the cursor.
func (crawler *MetaplexCrawler) GetCursorFromJournal() (uint64, error) {
	query := fmt.Sprintf("context_type:cursor context_id:%s", crawler.CursorName)
	parameters := map[string]string{
		"order":   "desc",
		"content": "false",
	}
	results, err := crawler.BugoutClient.Spire.SearchEntries(crawler.BugoutToken, crawler.BugoutJournalID, query, 1, 0, parameters)
	if err != nil {
		return 0, err
	}

	if results.TotalResults == 0 {
		return 0, nil
	}

	value, conversionErr := strconv.ParseUint(results.Results[0].ContextUrl, 10, 64)
	if conversionErr != nil {
		return 0, conversionErr
	}

	return value, nil
}

// Creates a new entry in the given journal representing the current state of the cursor with the given name.
// The entry is created with:
// - context_type: cursor
// - context_id: <cursorName>
// - contextUrl: <value>
// In the case of Solana, we are storing slot numbers as cursor values.
func (crawler *MetaplexCrawler) WriteCursorToJournal(value uint64) error {
	title := fmt.Sprintf("Metaplex crawler cursor: %s", crawler.CursorName)
	valueString := strconv.FormatUint(value, 10)
	entryContext := spire.EntryContext{
		ContextType: "cursor",
		ContextID:   crawler.CursorName,
		ContextURL:  valueString,
	}

	_, err := crawler.BugoutClient.Spire.CreateEntry(crawler.BugoutToken, crawler.BugoutJournalID, title, valueString, []string{}, entryContext)
	return err
}

func (crawler *MetaplexCrawler) Crawl() error {
	getSlotResult, getSlotErr := crawler.SolanaClient.GetSlot()
	if getSlotErr != nil {
		return getSlotErr
	}
	currentSlot := getSlotResult.Slot
	fmt.Printf("Current latest slot: %d\n", currentSlot)

	for {
		// Crawl until we catch up.
		startSlot := crawler.NextSlot
		endSlot := startSlot + maxSlotDifference

		fmt.Printf("Working through slots in range [%d, %d]\n", startSlot, endSlot)

		if startSlot > currentSlot {
			fmt.Printf("Starting slot (%d) exceeds head at start of crawl (%d): exiting\n", startSlot, currentSlot)
			break
		}

		result, getBlocksErr := crawler.SolanaClient.GetBlocks(startSlot, endSlot)
		if getBlocksErr != nil {
			return getBlocksErr
		}
		for _, blockNumber := range result.BlockNumbers {
			fmt.Printf("Processing block: %d\n", blockNumber)
			block, getBlockErr := crawler.SolanaClient.GetBlock(blockNumber)
			if getBlockErr != nil {
				return getBlockErr
			}
			for _, resolvedTransaction := range block.Transactions {
				var metaplexTransaction = false
				for _, accountKey := range resolvedTransaction.Transaction.Message.AccountKeys {
					if accountKey == crawler.MetaplexAccountKey {
						metaplexTransaction = true
						break
					}
				}

				if metaplexTransaction {
					fmt.Printf("Reporting transaction: %s\n", resolvedTransaction.Transaction.Message.AccountKeys[0])
					reportErr := crawler.ReportTransaction(block.ParentSlot, block.Blockhash, resolvedTransaction)
					if reportErr != nil {
						return reportErr
					}
				}
			}
		}

		crawler.NextSlot = endSlot + 1
		crawler.WriteCursorToJournal(crawler.NextSlot)
	}
	return nil
}

func (crawler *MetaplexCrawler) ReportTransaction(slot uint64, blockhash string, transaction ResolvedTransaction) error {
	if len(transaction.Transaction.Signatures) == 0 {
		return errors.New("transaction does not have a signature")
	}

	transactionSignature := transaction.Transaction.Signatures[0]

	status := "SUCCESS"
	if transaction.Meta.Err != nil {
		status = "FAILURE"
	}

	reportContext := spire.EntryContext{
		ContextType: "solana",
		ContextID:   crawler.MetaplexAccountKey,
		ContextURL:  "",
	}

	title := fmt.Sprintf("Transaction: %s -- %s", transactionSignature, status)
	contentBytes, encodeErr := json.MarshalIndent(transaction, "", "  ")
	if encodeErr != nil {
		return encodeErr
	}
	content := fmt.Sprintf("```json\n%s\n```\n", string(contentBytes))

	baseTags := []string{
		fmt.Sprintf("status:%s", status),
		fmt.Sprintf("client:%s", blockhash),
		fmt.Sprintf("slot:%d", slot),
	}
	numAccounts := len(transaction.Transaction.Message.AccountKeys) - 1
	tagsLength := len(baseTags) + numAccounts
	tags := make([]string, tagsLength)
	for i := 0; i < tagsLength; i++ {
		if i < len(baseTags) {
			tags[i] = baseTags[i]
		} else {
			tags[i] = fmt.Sprintf("session:%s", transaction.Transaction.Message.AccountKeys[1+i-len(baseTags)])
		}
	}

	_, reportingErr := crawler.BugoutClient.Spire.CreateEntry(crawler.BugoutToken, crawler.BugoutJournalID, title, content, tags, reportContext)
	return reportingErr
}

func NewCrawler(bugoutToken, bugoutJournalID, cursorName string, startSlot uint64) (*MetaplexCrawler, error) {
	crawler := MetaplexCrawler{
		BugoutToken:        bugoutToken,
		BugoutJournalID:    bugoutJournalID,
		CursorName:         cursorName,
		MetaplexAccountKey: MetaplexAccountKey,
		NextSlot:           startSlot,
	}
	timeout := time.Duration(5 * time.Second)
	solanaClient, solanaClientErr := NewSolanaClient(SolanaMainnetAPIURL, timeout, 4.0)
	if solanaClientErr != nil {
		return &crawler, solanaClientErr
	}
	crawler.SolanaClient = solanaClient

	bugoutClient, bugoutClientErr := bugout.ClientFromEnv()
	if bugoutClientErr != nil {
		return &crawler, bugoutClientErr
	}
	crawler.BugoutClient = &bugoutClient

	currentValue, currentValueErr := crawler.GetCursorFromJournal()
	if currentValueErr != nil {
		return &crawler, fmt.Errorf("could not get current cursor value (cursorName=%s) from journal (id=%s): %s", crawler.CursorName, crawler.BugoutJournalID, currentValueErr.Error())
	}
	if currentValue >= crawler.NextSlot {
		crawler.NextSlot = currentValue + 1
	}

	return &crawler, nil
}
