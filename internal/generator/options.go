package generator

type Options struct {
	Namespace string
	Kinds     []string
	Name      string
	Selector  string
	Harden    bool
	OutDir    string
	DryRun    bool
}
