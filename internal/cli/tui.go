package cli

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/tui"
)

// runTUI opens the interactive UI over the same client every flag command uses.
func runTUI(cmd *cobra.Command) error {
	e, err := loadEnv(cmd, true)
	if err != nil {
		return err
	}
	orgID, err := e.requireOrg()
	if err != nil {
		return err
	}
	clusterID, err := e.requireCluster()
	if err != nil {
		return err
	}

	// Naming the cluster in the header costs one request and prevents the most expensive
	// mistake this tool can enable: acting on the wrong cluster because the header showed
	// an opaque id.
	clusterName := ""
	if c, err := e.client.ClusterByID(cmd.Context(), orgID, clusterID); err == nil {
		clusterName = c.Name
	}

	model := tui.New(tui.Options{
		Client:      e.client,
		OrgID:       orgID,
		ClusterID:   clusterID,
		ClusterName: clusterName,
		ContextName: e.ctx.Name,
		APIURL:      e.client.BaseURL,
		Refresh:     flags.refresh,
		InitialView: flags.initialView,
	})

	p := tea.NewProgram(model, tea.WithAltScreen(), tea.WithContext(cmd.Context()))
	_, err = p.Run()
	return err
}
