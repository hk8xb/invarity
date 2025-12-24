package cli

import (
	"encoding/json"
	"fmt"
	"os"
	"runtime"

	"github.com/spf13/cobra"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Long:  `Displays the version of the Invarity CLI along with build information.`,
	RunE:  runVersion,
}

type versionInfo struct {
	Version   string `json:"version"`
	GoVersion string `json:"go_version"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

func runVersion(cmd *cobra.Command, args []string) error {
	info := versionInfo{
		Version:   Version,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}

	if cfgJSON {
		jsonOut, _ := json.MarshalIndent(info, "", "  ")
		printJSON(jsonOut)
		return nil
	}

	fmt.Fprintf(os.Stdout, "invarity version %s\n", info.Version)
	fmt.Fprintf(os.Stdout, "  Go version: %s\n", info.GoVersion)
	fmt.Fprintf(os.Stdout, "  OS/Arch:    %s/%s\n", info.OS, info.Arch)

	return nil
}
