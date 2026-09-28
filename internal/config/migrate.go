package config

type migrationStep func(cfg *Config)

var migrations = map[int]migrationStep{}

func migrateToCurrent(cfg *Config, fromVersion int) {
	if fromVersion < 0 {
		fromVersion = 0
	}
	for v := fromVersion; v < CurrentVersion; v++ {
		if step, ok := migrations[v]; ok {
			step(cfg)
		}
	}
	cfg.Version = CurrentVersion
}
