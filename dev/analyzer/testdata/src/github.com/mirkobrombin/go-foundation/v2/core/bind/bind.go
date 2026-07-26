package bind

type Binder struct{}

func New() *Binder {
	return &Binder{}
}

func (b *Binder) Bind(any) error {
	return nil
}

func (b *Binder) BindJSON(any, []byte) error {
	return nil
}
