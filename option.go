package codeChecker

type module struct {
	path        string
	localPrefix string
	withJoin    bool
	withImports bool
	logger      Logger
}

type Option interface {
	apply(*module)
}

type optionFunc func(*module)

func (o optionFunc) apply(c *module) {
	o(c)
}

func (m *module) apply(opts []Option) *module {
	for _, o := range opts {
		o.apply(m)
	}

	return m
}

type Logger interface {
	Print(v ...any)
	Println(v ...any)
	Printf(format string, v ...any)
}

func WithLogger(logger Logger) Option {
	return optionFunc(func(m *module) {
		m.logger = logger
	})
}

func WithJoinRequireModules() Option {
	return optionFunc(func(m *module) {
		m.withJoin = true
	})
}

func WithLocalPrefix(prefix string) Option {
	return optionFunc(func(m *module) {
		m.localPrefix = prefix
	})
}

func WithImports() Option {
	return optionFunc(func(m *module) {
		m.withImports = true
	})
}
