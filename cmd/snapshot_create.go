package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/charmbracelet/lipgloss"
	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"

	"github.com/Thunder-Compute/thunder-cli/api"
	"github.com/Thunder-Compute/thunder-cli/tui"
	helpmenus "github.com/Thunder-Compute/thunder-cli/tui/help-menus"
	"github.com/Thunder-Compute/thunder-cli/tui/theme"
)

var (
	snapshotInstanceID string
	snapshotName       string
)

var snapshotCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a snapshot from an instance",
	Args:  wrapArgs(cobra.NoArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runSnapshotCreate(cmd)
	},
}

func init() {
	snapshotCreateCmd.SetHelpFunc(wrapHelp(helpmenus.RenderSnapshotCreateHelp))

	snapshotCmd.AddCommand(snapshotCreateCmd)

	snapshotCreateCmd.Flags().StringVar(&snapshotInstanceID, "instance-id", "", "Instance ID or UUID to snapshot")
	snapshotCreateCmd.Flags().StringVar(&snapshotName, "name", "", "Name for the snapshot")
}

func performSnapshotCreate(client *api.Client, req api.CreateSnapshotRequest, resp **api.CreateSnapshotResponse) error {
	r, err := client.CreateSnapshot(req)
	if err == nil {
		*resp = r
	}
	return err
}

func renderSnapshotCreateSuccess(resp **api.CreateSnapshotResponse) func() string {
	return func() string {
		labelStyle := theme.Neutral()
		boxStyle := lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			Padding(1, 2)

		var lines []string
		successTitleStyle := theme.Success()
		lines = append(lines, successTitleStyle.Render("✓ Snapshot created successfully!"))
		lines = append(lines, "")
		if (*resp).Message != "" {
			lines = append(lines, labelStyle.Render("Message: ")+(*resp).Message)
		}

		content := lipgloss.JoinVertical(lipgloss.Left, lines...)
		return "\n" + boxStyle.Render(content) + "\n\n"
	}
}

func runSnapshotCreate(cmd *cobra.Command) error {
	isInteractive := tui.IsInteractive() && !JSONOutput &&
		!cmd.Flags().Changed("instance-id") && !cmd.Flags().Changed("name")
	if !isInteractive {
		if snapshotInstanceID == "" {
			return usageErr("--instance-id is required in non-interactive mode")
		}
		if snapshotName == "" {
			return usageErr("--name is required in non-interactive mode")
		}
	}

	client, err := getAuthenticatedClient()
	if err != nil {
		return err
	}

	var instanceID, name string

	if isInteractive {
		// Run interactive flow
		createConfig, err := tui.RunSnapshotCreateInteractive(client)
		if err != nil {
			if errors.Is(err, tui.ErrCancelled) {
				return nil
			}
			if errors.Is(err, tui.ErrNoRunningInstances) {
				PrintWarningSimple("No running instances found. Snapshots can only be created from instances in RUNNING state.")
				return nil
			}
			return err
		}
		instanceID = createConfig.InstanceID
		name = createConfig.Name
	} else {
		// Non-interactive mode: validate flags
		instanceID = snapshotInstanceID
		name = snapshotName

		// Validate instance exists and is in RUNNING state
		var instances []api.Instance
		if err := tui.RunWithBusySpinner("Validating instance...", os.Stdout, func() error {
			var e error
			instances, e = client.ListInstances()
			return e
		}); err != nil {
			return fmt.Errorf("failed to fetch instances: %w", err)
		}

		foundInstance := findInstance(instances, instanceID)

		if foundInstance == nil {
			return usageErr("instance '%s' not found", instanceID)
		}

		if foundInstance.Status != "RUNNING" {
			return usageErr("instance must be in RUNNING state to create snapshot (current state: %s)", foundInstance.Status)
		}

		// Use UUID for the API call
		instanceID = foundInstance.UUID
	}

	req := api.CreateSnapshotRequest{
		InstanceID: instanceID,
		Name:       name,
	}

	interactive := tui.IsInteractive() && !JSONOutput

	var snapshotResp *api.CreateSnapshotResponse

	if !interactive {
		fmt.Fprintln(os.Stderr, "Creating snapshot...")
		snapshotResp, err = client.CreateSnapshot(req)
		if err != nil {
			return fmt.Errorf("failed to create snapshot: %w", err)
		}
		if JSONOutput {
			printJSON(snapshotResp)
		} else {
			msg := "Snapshot created"
			if snapshotResp != nil && snapshotResp.Message != "" {
				msg = snapshotResp.Message
			}
			fmt.Println(msg)
		}
		return nil
	}

	runErr := tui.RunProgress("Creating snapshot...",
		func() error { return performSnapshotCreate(client, req, &snapshotResp) },
		renderSnapshotCreateSuccess(&snapshotResp),
	)
	if runErr != nil {
		sentry.WithScope(func(scope *sentry.Scope) {
			scope.SetTag("operation", "snapshot_create")
			if !isUserError(runErr) {
				sentry.CaptureException(runErr)
			}
		})
		return fmt.Errorf("failed to create snapshot: %w", runErr)
	}

	return nil
}
