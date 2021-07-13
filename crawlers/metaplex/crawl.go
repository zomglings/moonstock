package main

import (
	"fmt"
	"strconv"
	"time"

	bugout "github.com/bugout-dev/bugout-go/pkg"
	spire "github.com/bugout-dev/bugout-go/pkg/spire"
)

var SolanaMainnetAPIURL = "https://api.mainnet-beta.solana.com"

// Coordinates a crawl of the Solana blockchain looking for information about Metaplex
type MetaplexCrawler struct {
	SolanaClient    *SolanaClient
	BugoutClient    *bugout.BugoutClient
	BugoutToken     string
	BugoutJournalID string
	CursorName      string
	NextSlot        uint64
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
	return nil
}

func NewCrawler(bugoutToken, bugoutJournalID, cursorName string, startSlot uint64) (*MetaplexCrawler, error) {
	crawler := MetaplexCrawler{
		BugoutToken:     bugoutToken,
		BugoutJournalID: bugoutJournalID,
		CursorName:      cursorName,
		NextSlot:        startSlot,
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
