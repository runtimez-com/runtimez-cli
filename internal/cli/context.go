package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/config"
	"github.com/runtimez-com/runtimez-cli/internal/render"
)

func newWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show the signed-in identity and the resolved context",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			e, err := loadEnv(cmd, true)
			if err != nil {
				return err
			}
			me, err := e.client.Me(cmd.Context())
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(me, nil)
			}

			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "Email:      %s\n", render.Dash(str(me["email"])))
			fmt.Fprintf(out, "Role:       %s\n", render.Dash(str(me["role"])))
			fmt.Fprintf(out, "Org:        %s\n", render.Dash(firstNonEmpty(str(me["orgId"]), e.orgID)))
			fmt.Fprintf(out, "Cluster:    %s\n", render.Dash(e.cluster))
			fmt.Fprintf(out, "API:        %s\n", e.client.BaseURL)
			fmt.Fprintf(out, "Context:    %s\n", render.Dash(e.ctx.Name))
			fmt.Fprintf(out, "Credential: %s\n", e.creds.Kind)
			return nil
		},
	}
}

func newUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <cluster-id|cluster-name>",
		Short: "Select the cluster the current context points at",
		Args:  cobra.ExactArgs(1),
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

			want := args[0]
			var matches []string
			var chosen string
			for _, c := range clusters {
				if c.ID == want {
					chosen = c.ID
					matches = []string{c.ID}
					break
				}
				if strings.EqualFold(c.Name, want) {
					chosen = c.ID
					matches = append(matches, c.ID)
				}
			}
			switch {
			case chosen == "":
				return usageErrorf("no cluster named %q in org %s — run `rtz cluster ls`", want, orgID)
			case len(matches) > 1:
				return usageErrorf("%q matches %d clusters — use the cluster id instead", want, len(matches))
			}

			if e.ctx.Name == "" {
				return usageErrorf("no context selected — run `rtz login` first")
			}
			e.ctx.OrgID = orgID
			e.ctx.ClusterID = chosen
			e.cfg.Upsert(e.ctx)
			if err := e.cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Context %q now targets cluster %s\n", e.ctx.Name, chosen)
			return nil
		},
	}
}

func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Manage contexts",
	}

	cmd.AddCommand(&cobra.Command{
		Use:     "get-contexts",
		Aliases: []string{"ls"},
		Short:   "List configured contexts",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := printer(cmd)
			if err != nil {
				return err
			}
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			table := &render.Table{Headers: []string{"CURRENT", "NAME", "API", "ORG", "CLUSTER"}}
			for _, c := range cfg.Contexts {
				marker := ""
				if c.Name == cfg.CurrentContext {
					marker = "*"
				}
				table.Rows = append(table.Rows, []string{
					marker, c.Name, c.APIURL, render.Dash(c.OrgID), render.Dash(c.ClusterID),
				})
			}
			return p.Print(cfg.Contexts, table)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "use-context <name>",
		Short: "Select the current context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			if err := cfg.Use(args[0]); err != nil {
				return usageErrorf("%v", err)
			}
			if err := cfg.Save(); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Switched to context %q\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "delete-context <name>",
		Short: "Remove a context and its stored credentials",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg, err := config.Load()
			if err != nil {
				return err
			}
			cctx := cfg.Get(args[0])
			if cctx == nil {
				return usageErrorf("context %q not found", args[0])
			}
			ref := credentialRef(cctx)
			cfg.Remove(args[0])
			if err := cfg.Save(); err != nil {
				return err
			}
			_ = deleteCredentials(ref)
			fmt.Fprintf(cmd.OutOrStdout(), "Deleted context %q\n", args[0])
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "path",
		Short: "Print the config file location",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path, err := config.Path()
			if err != nil {
				return err
			}
			fmt.Fprintln(cmd.OutOrStdout(), path)
			return nil
		},
	})

	return cmd
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
