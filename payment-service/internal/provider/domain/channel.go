package domain

type DepositChannel struct {
	ID       int64
	Name     string
	Provider string
	Enabled  bool
}
