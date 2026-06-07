package skillexec

import (
	"github.com/elhenro/bee/internal/skills"
	"github.com/elhenro/bee/internal/tools"
)

// RegisterExecSkills registers every exec-kind skill in list as a model-callable
// tool, returning the count registered. Non-exec skills are ignored. A skill
// whose name collides with an already-registered tool is skipped (built-ins and
// earlier skills win) so a stray skill file can never clobber a core tool. A
// skill that fails to build (empty command) is skipped silently.
func RegisterExecSkills(r *tools.Registry, list []skills.Skill) int {
	n := 0
	for _, s := range list {
		if s.Kind != skills.KindExec {
			continue
		}
		if _, exists := r.Get(s.Name); exists {
			continue
		}
		t, err := New(s)
		if err != nil {
			continue
		}
		if err := r.Register(t); err != nil {
			continue
		}
		n++
	}
	return n
}
