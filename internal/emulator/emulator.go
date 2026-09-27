// Package emulator runs the Cosmos DB emulator in a container through the
// docker or podman command line. It only starts, stops or deletes a
// container carrying its label, and only deletes the data volume when asked.
package emulator

import (
	"errors"
	"fmt"
)

const (
	Image         = "mcr.microsoft.com/cosmosdb/linux/azure-cosmos-emulator:vnext-preview"
	ContainerName = "alchemist-cosmos-emulator"
	VolumeName    = "alchemist-cosmos-emulator-data"
	// ProfileName is the profile start writes and the bare command opens.
	ProfileName = "emulator"
	DefaultPort = 8081
	// Label marks a container Alchemist created, and so may stop or delete.
	Label = "dev.alchemist.emulator"

	containerPort = 8081
	dataPath      = "/data"
)

var (
	ErrNoRuntime = errors.New("found neither docker nor podman on PATH")
	// ErrNotManaged is a container of ContainerName without Label, such as
	// the one the old docker compose file made.
	ErrNotManaged  = errors.New("exists that Alchemist did not create")
	ErrPortInUse   = errors.New("is in use on this machine")
	ErrNoContainer = errors.New("no emulator container")
)

// Endpoint is the address the emulator answers on when published on port.
func Endpoint(port int) string {
	return fmt.Sprintf("http://localhost:%d", port)
}
