package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	humbug "github.com/bugout-dev/humbug/go/pkg"
	"github.com/google/uuid"
)

const Version = "0.0.1"

var SolanaMainnetAPIURL = "https://api.mainnet-beta.solana.com"

var reporterToken string = "189e7196-29d2-4159-95f7-3f210c6b6b14"

func main() {
	// Set up usage and crash reporting for the Metaplex crawler.
	// Opt-out consent flow for crash reporting. Set CRAWLER_REPORTING_ENABLED=0 to disable all reporting.
	// Alternatively, set BUGGER_OFF=true to turn off all reporting.
	consent := humbug.CreateHumbugConsent(humbug.EnvironmentVariableConsent("CRAWLER_REPORTING_ENABLED", humbug.No, true))

	// Set CRAWLER_REPORTING_SENDER to your email address or other contact information so that we
	// can contact you to get more information about your crash or when we have resolved the issue.
	clientID := os.Getenv("CRAWLER_REPORTING_SENDER")
	sessionID := uuid.NewString()

	reporter, err := humbug.CreateHumbugReporter(consent, clientID, sessionID, reporterToken)
	if err != nil {
		panic(err)
	}

	defer func() {
		message := recover()
		if message != nil {
			report := humbug.PanicReport(message)
			reporter.Publish(report)
			panic(message)
		}
	}()

	report := humbug.SystemReport()
	reporter.Publish(report)

	// Parse arguments from command line
	var requestsPerSecond float64
	var startSlot int64
	var bugoutJournalID, bugoutToken, cursorName string

	var checkVersion bool
	flag.Float64Var(&requestsPerSecond, "rate", 4, "Rate limit to apply when making requests to the Solana Cluster RPC API (units: requests per second)")
	flag.Int64Var(&startSlot, "start", -1, "Number of slot at which to start the crawl")
	flag.StringVar(&bugoutJournalID, "output", "", "Bugout.dev journal ID to write results of the crawl to")
	flag.StringVar(&bugoutToken, "token", "", "Bugout.dev access token (generate one at https://bugout.dev/account/tokens)")
	flag.StringVar(&cursorName, "cursor", "", "Name of cursor under which to persist the current crawl state - used for checkpointing")
	flag.BoolVar(&checkVersion, "version", false, "Set this flag to see the current version of the crawler and immediately exit")

	flag.Parse()

	if checkVersion {
		fmt.Println(Version)
		os.Exit(0)
	}

	if bugoutToken == "" {
		bugoutTokenEnvvar := "BUGOUT_ACCESS_TOKEN"
		bugoutTokenFromEnv := os.Getenv(bugoutTokenEnvvar)
		if bugoutTokenFromEnv != "" {
			fmt.Fprintf(os.Stderr, "Bugout access token was not specified using the -token argument. Using the value in the %s environment variable instead.\n", bugoutTokenEnvvar)
			bugoutToken = bugoutTokenFromEnv
		} else {
			log.Fatalf("No token specified at command line (using -token argument), and %s environment variable not set.\n", bugoutTokenEnvvar)
		}
	}

	if bugoutJournalID == "" {
		bugoutJournalIDEnvvar := "BUGOUT_JOURNAL_ID"
		bugoutJournalIDFromEnv := os.Getenv(bugoutJournalIDEnvvar)
		if bugoutJournalIDFromEnv != "" {
			fmt.Fprintf(os.Stderr, "Output journal ID was not specified using the -token argument. Using the value in the %s environment variable instead.\n", bugoutJournalIDEnvvar)
			bugoutJournalID = bugoutJournalIDFromEnv
		} else {
			log.Fatalf("No output journal specified at command line (using -output argument), and %s environment variable not set.\n", bugoutJournalIDEnvvar)
		}
	}

	timeout := time.Duration(5 * time.Second)
	solanaClient, solanaClientErr := NewSolanaClient(SolanaMainnetAPIURL, timeout, 4.0)
	if solanaClientErr != nil {
		panic(solanaClientErr)
	}
	block, err := solanaClient.GetBlock(86842651)
	if err != nil {
		panic(err)
	}
	fmt.Println(block)
}
