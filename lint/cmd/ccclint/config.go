package main

type Config struct {
	Linters LintersSettings `yaml:"linters-settings"`
}

type LintersSettings struct {
	Custom Custom `yaml:"custom"`
}

type Custom struct {
	CCCLint Linter `yaml:"ccclint"`
}

type Linter struct {
	Path string `yaml:"path"`
}
