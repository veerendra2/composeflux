package main

type RunCmd struct {
	CommonConfig `embed:""`
}

func (r *RunCmd) AfterApply() error {
	return r.Validate()
}

func (r *RunCmd) Run() error {
	rClient, ctx, cleanup, err := r.Setup()
	if err != nil {
		return err
	}
	defer cleanup()

	rClient.Run(ctx)
	return nil
}
