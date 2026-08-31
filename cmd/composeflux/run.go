package main

type RunCmd struct {
	CommonConfig `embed:""`
}

// AfterApply validates the shared configuration after CLI values are applied.
func (r *RunCmd) AfterApply() error {
	return r.Validate()
}

// Run starts the continuous reconciliation loop.
func (r *RunCmd) Run() error {
	rClient, ctx, cleanup, err := r.Setup()
	if err != nil {
		return err
	}
	defer cleanup()

	rClient.Run(ctx)
	return nil
}
