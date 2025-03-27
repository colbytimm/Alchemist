package util

import (
	"github.com/charmbracelet/log"
)

func SetupLogging(verbose bool) {
	log.SetReportTimestamp(false)
	if verbose {
		log.SetLevel(log.DebugLevel)
		log.Debug("Debug logging enabled")
	} else {
		log.SetLevel(log.InfoLevel)
	}
}
