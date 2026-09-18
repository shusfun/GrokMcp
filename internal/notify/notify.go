package notify

type Sender interface {
	Send(title, body string)
}

type Func func(title, body string)

func (f Func) Send(title, body string) {
	if f != nil {
		f(title, body)
	}
}

type Nop struct{}

func (Nop) Send(string, string) {}
