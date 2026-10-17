package config

// CronConfig declares scheduled jobs for `bee cron`. Jobs are plain shell
// commands (typically `bee run ...`); an empty list leaves cron idle.
//
//	[[cron.jobs]]
//	name         = "nightly-review"
//	schedule     = "0 2 * * *"
//	command      = "bee run --headless 'review yesterday commits'"
//	active_hours = "01:00-06:00"
type CronConfig struct {
	Jobs []CronJob `toml:"jobs"`
}

// CronJob is one `[[cron.jobs]]` entry. ResourceKey defaults to Name.
type CronJob struct {
	Name        string `toml:"name"`
	Schedule    string `toml:"schedule"`
	Command     string `toml:"command"`
	ActiveHours string `toml:"active_hours"`
	ResourceKey string `toml:"resource_key"`
}
