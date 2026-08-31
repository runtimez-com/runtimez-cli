package cli

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/runtimez-com/runtimez-cli/internal/view"
)

func newDescribeCmd() *cobra.Command {
	var namespace string
	var kind string

	cmd := &cobra.Command{
		Use:     "describe <namespace>/<name> | <name> -n <namespace>",
		Aliases: []string{"desc"},
		Short:   "Show a workload with its pods, events and related objects",
		Long: `One screen replacing describe + get pods + get events + get svc.

Events are correlated by the backend, warnings first, capped at 50.`,
		Example: `  rtz describe payments/checkout
  rtz describe checkout -n payments
  rtz describe payments/checkout -o json`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
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

			ns, name, err := splitWorkloadRef(args[0], namespace)
			if err != nil {
				return err
			}
			if kind != "" {
				if kind, err = resolveKind(kind); err != nil {
					return err
				}
			}

			detail, err := e.client.Detail(cmd.Context(), orgID, clusterID, ns, name, kind)
			if err != nil {
				return err
			}
			if e.printer.Format.Structured() {
				return e.printer.Print(detail, nil)
			}
			// A never-synced or misspelled workload comes back as a 200 with a null body
			// rather than a 404, so saying "not found" here is the only way it reads as a
			// miss instead of a healthy workload with nothing wrong.
			if detail.Workload == nil {
				return &ExitError{Code: ExitFailure, Err: fmt.Errorf(
					"no workload %s/%s in cluster %s (check the namespace, or `rtz search %s`)", ns, name, clusterID, name)}
			}
			view.Detail(cmd.OutOrStdout(), detail)
			return nil
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "namespace, when not given as namespace/name")
	cmd.Flags().StringVar(&kind, "kind", "", "disambiguate when two kinds share a name")
	return cmd
}

// splitWorkloadRef accepts either "ns/name" or a bare name plus -n.
func splitWorkloadRef(ref, namespace string) (string, string, error) {
	if ns, name, ok := strings.Cut(ref, "/"); ok {
		if ns == "" || name == "" {
			return "", "", usageErrorf("malformed reference %q — expected <namespace>/<name>", ref)
		}
		if namespace != "" && namespace != ns {
			return "", "", usageErrorf("conflicting namespaces: %q in the reference, %q in -n", ns, namespace)
		}
		return ns, name, nil
	}
	if namespace == "" {
		return "", "", usageErrorf("no namespace — use <namespace>/<name> or pass -n")
	}
	return namespace, ref, nil
}
