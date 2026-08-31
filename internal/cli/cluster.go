package cli

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/render"
)

func newClusterCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "cluster",
		Aliases: []string{"clusters"},
		Short:   "Clusters connected to your organization",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List clusters",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			clusters, err := e.client.Clusters(cmd.Context(), orgID)
			if err != nil {
				return err
			}

			table := &render.Table{
				Headers:     []string{"CURRENT", "ID", "NAME", "STATUS", "K8S", "NODES", "RESOURCES", "LAST HEARTBEAT"},
				WideHeaders: []string{"PROVIDER", "AGENT", "LAST SYNC"},
			}
			for _, c := range clusters {
				marker := ""
				if c.ID == e.cluster {
					marker = "*"
				}
				table.Rows = append(table.Rows, []string{
					marker,
					c.ID,
					render.Dash(c.Name),
					render.Dash(c.Status),
					render.Dash(c.KubernetesVersion),
					intOrDash(c.NodeCount),
					intOrDash(c.ResourceCount),
					age(c.LastHeartbeatAt),
				})
				table.WideRows = append(table.WideRows, []string{
					render.Dash(c.Provider), render.Dash(c.AgentVersion), age(c.LastSyncAt),
				})
			}
			return e.printer.Print(clusters, table)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "get [cluster-id]",
		Short: "Show one cluster in detail",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgID, err := e.requireOrg()
			if err != nil {
				return err
			}
			clusterID := e.cluster
			if len(args) == 1 {
				clusterID = args[0]
			}
			if clusterID == "" {
				return usageErrorf("no cluster selected — pass one, or run `rtz use <cluster>`")
			}
			c, err := e.client.ClusterByID(cmd.Context(), orgID, clusterID)
			if err != nil {
				return err
			}
			table := &render.Table{
				Headers: []string{"FIELD", "VALUE"},
				Rows: [][]string{
					{"ID", c.ID},
					{"Name", render.Dash(c.Name)},
					{"Status", render.Dash(c.Status)},
					{"Kubernetes", render.Dash(c.KubernetesVersion)},
					{"Provider", render.Dash(c.Provider)},
					{"Agent", render.Dash(c.AgentVersion)},
					{"Nodes", intOrDash(c.NodeCount)},
					{"Resources", intOrDash(c.ResourceCount)},
					{"Last heartbeat", age(c.LastHeartbeatAt)},
					{"Last sync", age(c.LastSyncAt)},
					{"Compliance", render.Dash(c.ComplianceFramework)},
				},
			}
			return e.printer.Print(c, table)
		},
	})

	return cmd
}

func intOrDash(v *int) string {
	if v == nil {
		return "<none>"
	}
	return itoa(*v)
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [20]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// age renders a timestamp the way kubectl does — elapsed time, because "4m ago" answers the
// operational question and an ISO string does not.
func age(t *time.Time) string {
	if t == nil || t.IsZero() {
		return "<never>"
	}
	d := time.Since(*t)
	switch {
	case d < 0:
		return "0s"
	case d < time.Minute:
		return itoa(int(d.Seconds())) + "s"
	case d < time.Hour:
		return itoa(int(d.Minutes())) + "m"
	case d < 24*time.Hour:
		return itoa(int(d.Hours())) + "h"
	default:
		return itoa(int(d.Hours()/24)) + "d"
	}
}
