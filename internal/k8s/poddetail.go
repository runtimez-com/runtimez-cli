package k8s

// The shapes here were read off the live agent payload, not assumed. A pod's spec carries
// containers[{name,image,resources{requests,limits},command,imagePullPolicy,livenessProbe,
// readinessProbe,startupProbe}], initContainers, nodeName, serviceAccountName; its status
// carries phase, qosClass, restartCount, lastRestartAt, containerStatuses[{name,image,ready,
// restartCount,state{type,startedAt},lastTerminated{reason,exitCode,finishedAt,message}}],
// initContainerStatuses and conditions.
//
// Notably ABSENT, so nothing here renders them: podIP, hostIP and startTime. The agent's
// curated flatten does not send them, and a column of blanks reads as "the pod has no IP".

// Container is one container, with its spec and its status merged.
type Container struct {
	Name  string
	Image string
	Init  bool

	Ready    bool
	Restarts int

	// State is the running/waiting/terminated word, with the reason when there is one —
	// "waiting: CrashLoopBackOff" is the whole answer to why a pod is not up.
	State       string
	StateReason string

	LastTerminatedReason string
	LastExitCode         int
	HasLastExitCode      bool
	LastFinishedAt       string

	CPURequest, CPULimit string
	MemRequest, MemLimit string

	// Probes names the probes that are configured, because a readiness probe that never
	// passes is the usual reason a Running pod is not Ready.
	Probes []string
}

// Condition is one pod condition.
type Condition struct {
	Type    string
	Status  string
	Reason  string
	Message string
}

// PodDetail is everything the describe pane shows for a single pod.
type PodDetail struct {
	Pod
	QoS               string
	ServiceAccount    string
	HostNetwork       bool
	Containers        []Container
	Conditions        []Condition
	MissingContainers bool // the payload carried no container detail at all
}

// ParsePodDetail merges a pod's spec and status into one view.
func ParsePodDetail(r Resource) PodDetail {
	spec := parse(r.Spec)
	status := parse(r.Status)

	d := PodDetail{
		Pod:            ParsePod(r),
		QoS:            status.str("qosClass"),
		ServiceAccount: spec.str("serviceAccountName"),
	}
	if hn, isBool := spec["hostNetwork"].(bool); isBool {
		d.HostNetwork = hn
	}

	d.Containers = append(d.Containers,
		mergeContainers(spec.list("initContainers"), status.list("initContainerStatuses"), true)...)
	d.Containers = append(d.Containers,
		mergeContainers(spec.list("containers"), status.list("containerStatuses"), false)...)
	d.MissingContainers = len(d.Containers) == 0

	for _, c := range status.list("conditions") {
		m, isMap := c.(map[string]any)
		if !isMap {
			continue
		}
		o := obj(m)
		d.Conditions = append(d.Conditions, Condition{
			Type: o.str("type"), Status: o.str("status"),
			Reason: o.str("reason"), Message: o.str("message"),
		})
	}
	return d
}

// mergeContainers joins spec and status entries by name. A container present in only one of
// them still appears: a status with no spec happens mid-rollout, and a spec with no status
// is a container that has not started, which is exactly what someone is looking for.
func mergeContainers(specs, statuses []any, init bool) []Container {
	byName := map[string]*Container{}
	order := []string{}

	get := func(name string) *Container {
		if c, ok := byName[name]; ok {
			return c
		}
		c := &Container{Name: name, Init: init}
		byName[name] = c
		order = append(order, name)
		return c
	}

	for _, raw := range specs {
		m, isMap := raw.(map[string]any)
		if !isMap {
			continue
		}
		o := obj(m)
		c := get(o.str("name"))
		c.Image = o.str("image")
		res := o.child("resources")
		req, lim := res.child("requests"), res.child("limits")
		c.CPURequest, c.CPULimit = req.str("cpu"), lim.str("cpu")
		c.MemRequest, c.MemLimit = req.str("memory"), lim.str("memory")
		for _, p := range []string{"livenessProbe", "readinessProbe", "startupProbe"} {
			if on, isBool := o[p].(bool); isBool && on {
				c.Probes = append(c.Probes, probeShortName(p))
			}
		}
	}

	for _, raw := range statuses {
		m, isMap := raw.(map[string]any)
		if !isMap {
			continue
		}
		o := obj(m)
		c := get(o.str("name"))
		if c.Image == "" {
			c.Image = o.str("image")
		}
		if ready, isBool := o["ready"].(bool); isBool {
			c.Ready = ready
		}
		c.Restarts, _ = o.num("restartCount")

		st := o.child("state")
		c.State = st.str("type")
		c.StateReason = st.str("reason")

		// lastState is the standard k8s key; the agent flattens it to lastTerminated.
		last := o.child("lastTerminated")
		if len(last) == 0 {
			last = o.child("lastState").child("terminated")
		}
		c.LastTerminatedReason = last.str("reason")
		c.LastFinishedAt = last.str("finishedAt")
		if code, ok := last.num("exitCode"); ok {
			c.LastExitCode, c.HasLastExitCode = code, true
		}
	}

	out := make([]Container, 0, len(order))
	for _, n := range order {
		out = append(out, *byName[n])
	}
	return out
}

func probeShortName(key string) string {
	switch key {
	case "livenessProbe":
		return "liveness"
	case "readinessProbe":
		return "readiness"
	case "startupProbe":
		return "startup"
	}
	return key
}

// StateLabel is the state with its reason folded in.
func (c Container) StateLabel() string {
	switch {
	case c.State == "" && c.StateReason == "":
		return "<unknown>"
	case c.StateReason == "":
		return c.State
	case c.State == "":
		return c.StateReason
	default:
		return c.State + ": " + c.StateReason
	}
}

// Resources renders "10m/50m" style request/limit pairs, and says which side is unset —
// an unlimited container is a fact worth seeing, not a blank.
func pairOrDash(request, limit string) string {
	if request == "" && limit == "" {
		return "-"
	}
	r, l := request, limit
	if r == "" {
		r = "-"
	}
	if l == "" {
		l = "-"
	}
	return r + "/" + l
}

func (c Container) CPU() string { return pairOrDash(c.CPURequest, c.CPULimit) }
func (c Container) Mem() string { return pairOrDash(c.MemRequest, c.MemLimit) }
