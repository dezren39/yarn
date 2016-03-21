// Package yarn implements the YarnSpinner VM (see github.com/thesecretlab/YarnSpinner).
package yarn

// ByteCode represents the operations the VM can perform.
type ByteCode int

const (
	ByteCodeLabel         = ByteCode(iota) // opA = string: label name
	ByteCodeJumpTo                         // opA = string: label name
	ByteCodeJump                           // peek string from stack and jump to that label
	ByteCodeRunLine                        // opA = int: string number
	ByteCodeRunCommand                     // opA = string: command text
	ByteCodeAddOption                      // opA = int: string number for option to add
	ByteCodeShowOptions                    // present the current list of options, then clear the list; most recently selected option will be on the top of the stack
	ByteCodePushString                     // opA = int: string number in table; push string to stack
	ByteCodePushNumber                     // opA = float: number to push to stack
	ByteCodePushBool                       // opA = int (0 or 1): bool to push to stack
	ByteCodePushNull                       // pushes a null value onto the stack
	ByteCodeJumpIfFalse                    // opA = string: label name if top of stack is not null, zero or false, jumps to that label
	ByteCodePop                            // discard top of stack
	ByteCodeCallFunc                       // opA = string; looks up function, pops as many arguments as needed, result is pushed to stack
	ByteCodePushVariable                   // opA = name of variable to get value of and push to stack
	ByteCodeStoreVariable                  // opA = name of variable to store top of stack in
	ByteCodeStop                           // stops execution
	ByteCodeRunNode                        // run the node whose name is at the top of the stack

)

// ExecState is the highest-level machine state.
type ExecState int

const (
	ExecStateStopped = ExecState(iota)
	ExecStateWaitOnOptionSelection
	ExecStateRunning
)

// VMState models a machine state.
type VMState struct {
	node    string
	pc      int
	stack   []interface{}
	options []option
}

// Push pushes a value onto the state's stack.
func (m *VMState) Push(x interface{}) { m.stack = append(m.stack, x) }

// Pop removes a value from the stack and returns it.
func (m *VMState) Pop() (interface{}, error) {
	x, err := m.Peek()
	if err != nil {
		return nil, err
	}
	m.stack = m.stack[:len(m.stack)-1]
	return x, nil
}

// Peek returns the top vaue from the stack only.
func (m *VMState) Peek() (interface{}, error) {
	if len(m.stack) == 0 {
		return nil, errors.New("stack underflow")
	}
	return m.stack[len(m.stack)-1], nil
}

// Clear resets the stack state.
func (m *VMState) Clear() { m.stack = nil }

// Instruction models a single yarn machine instruction.
type Instruction struct {
	bc       ByteCode
	opA, opB interface{}
}

// Node models a yarn node, which is a mini program.
type Node struct {
	code        []Instruction
	name        string
	sourceStrID string
	labels      map[string]int
}

// Program models an entire yarn program.
type Program struct {
	stringTable map[string]string
	nodeTable   map[string]*Node
}

// Function represents a callable function from the VM.
type Function interface {
	Invoke(params ...interface{}) (interface{}, error)
	ParamCount() int
	Returns() bool
}

// Library is a collection of functions callable from the VM.
type Library interface {
	Function(name string) (Function, error)
}

// VariableStorage stores numeric variables.
type VariableStorage interface {
	Set(name string, value float64)
	Get(name) (value float64, ok bool)
	Clear()
}

// Delegate receives events from the VM.
type Delegate interface {
	Line(line string) error                                              // handle a line of dialogue
	Command(command string) error                                        // handle a comment
	Options(options []string, pickedOption func(option int) error) error // user picks an option
	NodeComplete(nextNode string)                                        // this node is complete
}

type option struct{ id, node string }

// VM implements the virtual machine.
type VM struct {
	es ExecState
	p  *Program
	s  *VMState
	Delegate
	Library
	VariableStorage
}

// Stop stops the virtual machine.
func (m *VM) Stop() { m.es = ExecStateStopped }

// RunNext executes the next instruction in the current node.
func (m *VM) RunNext() error {
	switch m.es {
	case ExecStateStopped:
		m.es = ExecStateRunning
	case ExecStateWaitOnOptionSelection:
		return errors.New("cannot run, waiting on option selection")
	}
	if m.Delegate == nil {
		return errors.New("delegate is nil")
	}
	if m.VariableStorage == nil {
		return errors.New("variable storage is nil")
	}
	node, ok := m.p.nodeTable[m.s.node]
	if !ok {
		return fmt.Errorf("illegal state; unknown node of program %q", m.s.node)
	}
	if m.s.pc < 0 || m.s.pc >= len(node.code) {
		return fmt.Errorf("illegal state; pc %d outside program [0, %d)", m.s.pc, len(node.code))
	}
	ins := node.code[m.s.pc]
	if err := m.Execute(ins, node); err != nil {
		return err
	}
	m.s.pc++
	if m.s.pc >= len(node.code) {
		m.es = ExecStateStopped
	}
}

func (m *VM) optionPicked(i int) error {
	if m.es != ExecStateWaitOnOptionSelection {
		return fmt.Errorf("machine is not waiting for an option selection [m.es = %d]", m.es)
	}
	if i < 0 || i >= len(m.s.options) {
		return fmt.Errorf("selected option %d out of bounds [0, %d)", i, len(m.s.options))
	}
	m.s.Push(m.s.options[i].node)
	m.s.options = nil
	m.es = ExecStateRunning
	return nil
}

func convertToBool(x interface{}) (bool, error) {
	if x == nil {
		return false, nil
	}
	switch t := x.(type) {
	case bool:
		return t, nil
	case float64:
		return t != 0, nil
	case int:
		return t != 0, nil
	case string:
		return len(t) > 0, nil
	default:
		if t == nil {
			return false, nil
		}
		return false, fmt.Errorf("cannot convert value of type %T to a bool", x)
	}
}

// Execute executes a single instruction.
func (m *VM) Execute(i Instruction, node *Node) error {
	switch i.bc {
	case ByteCodeLabel:
		// nop

	case ByteCodeJumpTo:
		k, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		pc, ok := node.labels[k]
		if !ok {
			return fmt.Errorf("unknown label %q", k)
		}
		m.s.pc = pc

	case ByteCodeJump:
		o, err := m.s.Peek()
		if err != nil {
			return err
		}
		k, ok := o.(string)
		if !ok {
			return fmt.Errorf("wrong type of value at top of stack [%T != string]", o)
		}
		pc, ok := node.labels[k]
		if !ok {
			return fmt.Errorf("unknown label %q", k)
		}
		m.s.pc = pc

	case ByteCodeRunLine:
		x, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		l, ok := m.p.stringTable[x]
		if !ok {
			return fmt.Errorf("no string in string table for key %q", x)
		}
		if err := m.Line(l); err != nil {
			return err
		}

	case ByteCodeRunCommand:
		c, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		if err := m.Command(c); err != nil {
			return err
		}

	case ByteCodeAddOption:
		a, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		b, ok := i.opB.(string)
		if !ok {
			return fmt.Errorf("wrong type in opB [%T != string]", i.opB)
		}
		m.s.options = append(m.s.options, option{id: a, node: b})

	case ByteCodeShowOptions:
		switch len(m.s.options) {
		case 0:
			// NOTE: jon implements this as a machine stop instead of an exception
			return errors.New("illegal state, no options to show")
		case 1:
			m.s.Push(m.s.options[0].node)
			m.s.options = nil
			return nil
		}
		// TODO: implement shuffling of options depending on configuration.
		ops := make([]string, 0, len(m.s.options))
		for _, op := range m.s.options {
			s, ok = m.p.stringTable[op.id]
			if !ok {
				return fmt.Errorf("no string in string table for key %q", op.id)
			}
			ops = append(ops, s)
		}
		m.es = ExecStateWaitOnOptionSelection
		if err := m.Options(ops, m.optionPicked); err != nil {
			return err
		}

	case ByteCodePushString:
		x, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		s, ok := m.p.stringTable[x]
		if !ok {
			return fmt.Errorf("no string in string table for key %q", x)
		}
		m.s.Push(s)

	case ByteCodePushNumber:
		x, ok := i.opA.(float64)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != float64]", i.opA)
		}
		m.s.Push(x)

	case ByteCodePushBool:
		x, ok := i.opA.(bool)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != bool]", i.opA)
		}
		m.s.Push(x)

	case ByteCodePushNull:
		m.s.Push(nil)

	case ByteCodeJumpIfFalse:
		x, err := m.s.Peek()
		if err != nil {
			return err
		}
		b, err := convertToBool(x)
		if err != nil {
			return err
		}
		if b {
			return nil
		}
		k, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		pc, ok := node.labels[k]
		if !ok {
			return fmt.Errorf("unknown label %q", k)
		}
		m.s.pc = pc

	case ByteCodePop:
		m.s.Pop()

	case ByteCodeCallFunc:
		k, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		f, err := m.Library.Function(k)
		if err != nil {
			return err
		}
		c := f.ParamCount()
		if c == -1 {
			// Variadic, so param count is on stack.
			x, err := m.s.Pop()
			if err != nil {
				return err
			}
			y, ok := x.(int)
			if !ok {
				return fmt.Errorf("wrong type popped from stack [%T != int]", x)
			}
			c = y
		}
		params := make([]interface{}, c)
		for c >= 0 {
			c--
			p, err := m.s.Pop()
			if err != nil {
				return err
			}
			params[c] = p
		}
		r, err := f.Invoke(params...)
		if err != nil {
			return err
		}
		if f.Returns() {
			m.s.Push(r)
		}

	case ByteCodePushVariable:
		k, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		v, ok := m.VariableStorage.Get(k)
		if !ok {
			return fmt.Errorf("no variable called %q", k)
		}
		m.s.Push(v)

	case ByteCodeStoreVariable:
		k, ok := i.opA.(string)
		if !ok {
			return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
		}
		v, err := m.s.Peek()
		if err != nil {
			return err
		}
		m.VariableStorage.Set(k, v)

	case ByteCodeStop:
		m.es = ExecStateStopped
		// TODO: report execution stopped?

	case ByteCodeRunNode:
		node := ""
		if i.opA == nil || i.opA.(string) == "" {
			// Use the stack, Luke.
			n, err := m.s.Peek()
			if err != nil {
				return err
			}
			node = n
		} else {
			n, ok := i.opA.(string)
			if !ok {
				return fmt.Errorf("wrong type in opA [%T != string]", i.opA)
			}
			node = n
		}
		// TODO: completion handler
		m.s.node = node

	default:
		return fmt.Errorf("invalid instruction %d", i.bc)
	}
	return nil
}
