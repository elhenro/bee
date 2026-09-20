package cron

import "fmt"

// JobSpec is a config-defined job declaration. The on-disk format (TOML and
// friends) is the caller's concern; the registry parses and validates the
// fields a scheduler needs.
type JobSpec struct {
	Name        string
	Schedule    string
	Command     string
	ActiveHours string
	ResourceKey string
}

// Registry holds the jobs declared in config, keyed by name.
type Registry struct {
	jobs   []*Job
	byName map[string]*Job
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry { return &Registry{byName: make(map[string]*Job)} }

// Add parses and registers one spec. Name and Schedule are required; an empty
// ResourceKey defaults to the job name.
func (r *Registry) Add(spec JobSpec) (*Job, error) {
	if spec.Name == "" {
		return nil, fmt.Errorf("cron: job name is required")
	}
	if _, dup := r.byName[spec.Name]; dup {
		return nil, fmt.Errorf("cron: duplicate job name %q", spec.Name)
	}
	if spec.Schedule == "" {
		return nil, fmt.Errorf("cron: job %q: schedule is required", spec.Name)
	}
	sched, err := ParseSchedule(spec.Schedule)
	if err != nil {
		return nil, fmt.Errorf("cron: job %q: %w", spec.Name, err)
	}
	var win *Window
	if spec.ActiveHours != "" {
		win, err = ParseWindow(spec.ActiveHours)
		if err != nil {
			return nil, fmt.Errorf("cron: job %q: %w", spec.Name, err)
		}
	}
	key := spec.ResourceKey
	if key == "" {
		key = spec.Name
	}
	j := &Job{
		Name:        spec.Name,
		Command:     spec.Command,
		Schedule:    sched,
		ResourceKey: key,
		Window:      win,
	}
	r.jobs = append(r.jobs, j)
	r.byName[spec.Name] = j
	return j, nil
}

// Jobs returns the registered jobs in declaration order.
func (r *Registry) Jobs() []*Job { return append([]*Job(nil), r.jobs...) }

// Len returns the number of registered jobs.
func (r *Registry) Len() int { return len(r.jobs) }
