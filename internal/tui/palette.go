package tui

import (
	"sort"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The command bar is also the map of the UI. Typing `:` into an empty prompt asks the
// operator to already know the vocabulary; listing it, narrowing as they type, is what lets
// someone who has never read the help find the screen they need.

// command is one entry in the command bar. Name is what the menu fills in; Aliases also
// resolve. Arg, when set, is the argument the command takes ("<name>").
type command struct {
	Group   string
	Name    string
	Aliases []string
	Arg     string
	Desc    string
}

// catalog is every command the bar accepts, in the order the menu and the help screen list
// them. TestCatalogMatchesTheViewRegistry keeps it and the views map in step.
var catalog = []command{
	{Group: "Resources", Name: "pods", Aliases: []string{"po"}, Desc: "Pods"},
	{Group: "Resources", Name: "deploy", Aliases: []string{"deployments"}, Desc: "Deployments"},
	{Group: "Resources", Name: "sts", Aliases: []string{"statefulsets"}, Desc: "StatefulSets"},
	{Group: "Resources", Name: "ds", Aliases: []string{"daemonsets"}, Desc: "DaemonSets"},
	{Group: "Resources", Name: "svc", Aliases: []string{"services"}, Desc: "Services"},
	{Group: "Resources", Name: "ing", Aliases: []string{"ingresses"}, Desc: "Kubernetes Ingresses"},
	{Group: "Resources", Name: "nodes", Aliases: []string{"no"}, Desc: "Nodes"},
	{Group: "Resources", Name: "jobs", Desc: "Jobs"},
	{Group: "Resources", Name: "all", Desc: "Every kind in one list"},

	{Group: "Reliability", Name: "triage", Desc: "What is failing right now"},
	{Group: "Reliability", Name: "risk", Desc: "Workload risk score and the rules behind it"},
	{Group: "Reliability", Name: "rca", Desc: "Root cause for the selected workload"},
	{Group: "Reliability", Name: "signals", Desc: "Slow operations"},
	{Group: "Reliability", Name: "changes", Desc: "What changed, newest first"},

	{Group: "Observability", Name: "logs", Desc: "Log search, scoped to the selection"},
	{Group: "Observability", Name: "traces", Desc: "Traces, scoped to the selection"},

	{Group: "Capacity & lifecycle", Name: "capacity", Aliases: []string{"cap"}, Desc: "Node pools, headroom and pending pods"},
	{Group: "Capacity & lifecycle", Name: "pending", Desc: "Pods the scheduler could not place, and why"},
	{Group: "Capacity & lifecycle", Name: "storage", Aliases: []string{"pvc"}, Desc: "Volume claims, fullest first"},
	{Group: "Capacity & lifecycle", Name: "maintain", Aliases: []string{"eol"}, Desc: "Support deadlines for your workloads, soonest first"},
	{Group: "Capacity & lifecycle", Name: "upgrade", Desc: "What each app must change before the next Kubernetes upgrade"},

	{Group: "Add-ons", Name: "karpenter", Aliases: []string{"nodepools"}, Desc: "Karpenter NodePools: node provisioning and scaling"},
	{Group: "Add-ons", Name: "ca", Desc: "Cluster Autoscaler node groups and scaling"},
	{Group: "Add-ons", Name: "traefik", Aliases: []string{"routes"}, Desc: "Traefik ingress routes"},
	{Group: "Add-ons", Name: "istio", Aliases: []string{"mesh"}, Desc: "Istio mesh workloads"},
	{Group: "Add-ons", Name: "argocd", Aliases: []string{"gitops"}, Desc: "Argo CD Applications"},
	{Group: "Add-ons", Name: "cni", Aliases: []string{"vpccni"}, Desc: "Amazon VPC CNI nodes and IP capacity"},
	{Group: "Add-ons", Name: "ebs", Aliases: []string{"ebscsi"}, Desc: "Amazon EBS CSI nodes and volume attachments"},

	{Group: "Navigation", Name: "ns", Aliases: []string{"namespace"}, Arg: "<name>", Desc: "Scope to a namespace; bare :ns shows all"},
	{Group: "Navigation", Name: "q", Aliases: []string{"quit", "exit"}, Desc: "Quit"},
}

// names is the command's name followed by its aliases.
func (c command) names() []string { return append([]string{c.Name}, c.Aliases...) }

// matchCommands ranks the catalog against what is typed. An empty query is the whole
// catalog in its own order. Otherwise: an exact name or alias, then a name prefix, an alias
// prefix, a substring of either, a word in the description, and last a subsequence — so a
// typo like "kapenter" still finds karpenter. Ties keep catalog order.
func matchCommands(query string) []command {
	q := strings.ToLower(strings.TrimSpace(strings.TrimPrefix(query, ":")))
	if q == "" {
		return append([]command(nil), catalog...)
	}
	// Once there is an argument the name is settled: show that command and what it takes.
	if name, _, hasArg := strings.Cut(q, " "); hasArg {
		for _, c := range catalog {
			for _, n := range c.names() {
				if n == name {
					return []command{c}
				}
			}
		}
		return nil
	}

	type ranked struct {
		c     command
		score int
		pos   int
	}
	var out []ranked
	for i, c := range catalog {
		if s, ok := commandScore(c, q); ok {
			out = append(out, ranked{c, s, i})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].score < out[j].score })
	cmds := make([]command, len(out))
	for i, r := range out {
		cmds[i] = r.c
	}
	return cmds
}

func commandScore(c command, q string) (int, bool) {
	best := -1
	consider := func(s int) {
		if best < 0 || s < best {
			best = s
		}
	}
	for i, n := range c.names() {
		switch {
		case n == q:
			consider(0)
		case strings.HasPrefix(n, q) && i == 0:
			consider(1)
		case strings.HasPrefix(n, q):
			consider(2)
		case strings.Contains(n, q):
			consider(3)
		case len(q) >= 3 && isSubsequence(q, n):
			consider(5)
		}
	}
	if strings.Contains(strings.ToLower(c.Desc), q) {
		consider(4)
	}
	return best, best >= 0
}

func isSubsequence(q, s string) bool {
	i := 0
	for _, r := range s {
		if i < len(q) && rune(q[i]) == r {
			i++
		}
	}
	return i == len(q)
}

// suggestCommand is the nearest known name to a mistyped one, for "did you mean".
func suggestCommand(name string) string {
	best, bestDist := "", 3 // more than two edits away is a different word, not a typo
	for _, c := range catalog {
		for _, n := range c.names() {
			if d := editDistance(name, n); d < bestDist {
				best, bestDist = c.Name, d
			}
		}
	}
	return best
}

func editDistance(a, b string) int {
	prev := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		cur := make([]int, len(b)+1)
		cur[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(b)]
}

// paletteMatches is what the open command bar is offering right now.
func (m Model) paletteMatches() []command { return matchCommands(m.input.Value()) }

// paletteView takes the table's place while the command bar is open, at the same height, so
// opening it never moves the frame.
func (m Model) paletteView() string {
	height := m.bodyHeight() + 1
	if m.height > 0 {
		// Everything but the tab strip, the menu's own border and the two footer lines.
		height = max(m.height-1-panelHeight(0, m.screenWidth())-2, 1)
	}
	matches := m.paletteMatches()
	q := strings.TrimSpace(m.input.Value())
	grouped := q == ""

	var lines []string
	selLine := 0
	switch {
	case len(matches) == 0:
		msg := "No command matches " + `"` + q + `"` + " — enter runs it as typed, esc cancels."
		if s := suggestCommand(strings.ToLower(strings.Fields(q + " ")[0])); s != "" {
			msg = "No command matches " + `"` + q + `"` + " — did you mean :" + s + "?"
		}
		lines = append(lines, metaStyle.Render(msg))
	default:
		group := ""
		for i, c := range matches {
			if grouped && c.Group != group {
				if group != "" {
					lines = append(lines, "")
				}
				group = c.Group
				lines = append(lines, headerStyle.Render(group))
			}
			if i == m.palSel {
				selLine = len(lines)
			}
			lines = append(lines, m.paletteRow(c, i == m.palSel, !grouped))
		}
		if strings.Contains(q, " ") {
			lines = append(lines, "", metaStyle.Render("enter runs :"+q))
		}
	}

	// Keep the highlighted row on screen: scroll the window, never the frame.
	start := 0
	if selLine >= height {
		start = selLine - height + 1
	}
	end := min(start+height, len(lines))
	body := strings.Join(lines[start:end], "\n")
	if n := end - start; n < height {
		body += strings.Repeat("\n", height-n)
	}

	title := "commands"
	if !grouped {
		title += " [" + itoa(len(matches)) + "/" + itoa(len(catalog)) + "]"
	}
	return panel(title, body, m.screenWidth())
}

func (m Model) paletteRow(c command, selected, showGroup bool) string {
	name := c.Name
	if c.Arg != "" {
		name += " " + c.Arg
	}
	aliases := strings.Join(c.Aliases, " ")
	marker := "  "
	nameCell := keyStyle.Render(lipgloss.NewStyle().Width(15).Render(name))
	if selected {
		marker = keyStyle.Render("▸ ")
		nameCell = selStyle.Render(lipgloss.NewStyle().Width(15).Render(name))
	}
	row := marker + nameCell + " " + metaStyle.Render(lipgloss.NewStyle().Width(22).Render(clip(aliases, 21))) + c.Desc
	if showGroup {
		row += metaStyle.Render("  · " + c.Group)
	}
	return row
}

type commandGroup struct {
	name  string
	names []string
}

// commandGroups is the catalog folded by group, for the help screen.
func commandGroups() []commandGroup {
	var out []commandGroup
	for _, c := range catalog {
		if len(out) == 0 || out[len(out)-1].name != c.Group {
			out = append(out, commandGroup{name: c.Group})
		}
		n := ":" + c.Name
		if c.Arg != "" {
			n += " " + c.Arg
		}
		out[len(out)-1].names = append(out[len(out)-1].names, n)
	}
	return out
}
