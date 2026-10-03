// Code generated for instance allocation; see instance_alloc.go in git history for the generator note.
package value

// AllocInstance returns an Instance of cls whose Fields slice has n zero Values, allocating the
// header and the fields together (one allocation, sized exactly) for small n. Compared with a
// header plus a separately allocated Fields slice this saves the slice allocation and, for n >= 2,
// the unused inline storage of the previous layout.
func AllocInstance(cls *Class, n int) *Instance {
	switch n {
	case 0:
		return &Instance{Class: cls}
	case 1:
		x := &instanceN1{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	case 2:
		x := &instanceN2{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	case 3:
		x := &instanceN3{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	case 4:
		x := &instanceN4{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	case 5:
		x := &instanceN5{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	case 6:
		x := &instanceN6{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	case 7:
		x := &instanceN7{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	case 8:
		x := &instanceN8{}
		x.Class = cls
		x.Fields = x.f[:]
		return &x.Instance
	}
	return &Instance{Class: cls, Fields: make([]Value, n)}
}

type instanceN1 struct {
	Instance
	f [1]Value
}

type instanceN2 struct {
	Instance
	f [2]Value
}

type instanceN3 struct {
	Instance
	f [3]Value
}

type instanceN4 struct {
	Instance
	f [4]Value
}

type instanceN5 struct {
	Instance
	f [5]Value
}

type instanceN6 struct {
	Instance
	f [6]Value
}

type instanceN7 struct {
	Instance
	f [7]Value
}

type instanceN8 struct {
	Instance
	f [8]Value
}
