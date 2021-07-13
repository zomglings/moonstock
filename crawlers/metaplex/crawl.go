package main

import (
	"fmt"

	bugout "github.com/bugout-dev/bugout-go/pkg"
	spire "github.com/bugout-dev/bugout-go/pkg/spire"
)

// Gets the value of a cursor from the given journal. The value is assumed to be stored in the
// ContextUrl field of the journal entry representing the cursor.
func GetCursorFromJournal(client bugout.BugoutClient, bugoutToken, bugoutJournalID, cursorName string) (string, error) {
	query := fmt.Sprintf("context_type:cursor context_id:%s", cursorName)
	parameters := map[string]string{
		"order":   "desc",
		"content": "false",
	}
	results, err := client.Spire.SearchEntries(bugoutToken, bugoutJournalID, query, 1, 0, parameters)
	if err != nil {
		return "", err
	}

	if results.TotalResults == 0 {
		return "", nil
	}

	return results.Results[0].ContextUrl, nil
}

// Creates a new entry in the given journal representing the current state of the cursor with the given name.
// The entry is created with:
// - context_type: cursor
// - context_id: <cursorName>
// - contextUrl: <value>
// In the case of Solana, we are storing slot numbers as cursor values.
func WriteCursorToJournal(client bugout.BugoutClient, bugoutToken, bugoutJournalID, cursorName, value string) error {
	title := fmt.Sprintf("Metaplex crawler cursor: %s", cursorName)
	entryContext := spire.EntryContext{
		ContextType: "cursor",
		ContextID:   cursorName,
		ContextURL:  value,
	}

	_, err := client.Spire.CreateEntry(bugoutToken, bugoutJournalID, title, value, []string{}, entryContext)
	return err
}
