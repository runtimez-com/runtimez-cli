package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/render"
)

func newOrgCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "org",
		Aliases: []string{"orgs"},
		Short:   "Organizations you belong to",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List organizations",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			orgs, err := e.client.MyOrgs(cmd.Context())
			if err != nil {
				return err
			}
			table := &render.Table{Headers: []string{"CURRENT", "ORG ID", "NAME", "PLAN", "ROLE"}}
			for _, o := range orgs {
				marker := ""
				if o.Current {
					marker = "*"
				}
				table.Rows = append(table.Rows, []string{
					marker, o.OrgID, render.Dash(o.Name), render.Dash(o.Plan), render.Dash(o.Role),
				})
			}
			return e.printer.Print(orgs, table)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "use <org-id>",
		Short: "Point the current context at another organization",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			if e.ctx.Name == "" {
				return usageErrorf("no context selected — run `rtz login` first")
			}

			// Selecting an org invalidates the cluster: cluster ids are org-scoped, so
			// carrying the old one over would silently target something the new org cannot see.
			e.ctx.OrgID = args[0]
			e.ctx.ClusterID = ""
			e.cfg.Upsert(e.ctx)
			if err := e.cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(),
				"Context %q now targets org %s (cluster cleared — run `rtz cluster ls`)\n",
				e.ctx.Name, args[0])
			return nil
		},
	})

	return cmd
}
