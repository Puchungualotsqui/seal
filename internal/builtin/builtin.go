package builtin

type TypeClass int

const (
	TypeBool TypeClass = iota
	TypeSignedInt
	TypeUnsignedInt
	TypeFloat
	TypeChar
	TypeRawptr
	TypeAny
	TypeString
	TypeCString
)

type TypeSpec struct {
	Name  string
	Class TypeClass
	CName string

	AnyKind  string
	AnyField string
	AnyCtor  string
}

func (t TypeSpec) IsInteger() bool {
	return t.Class == TypeSignedInt || t.Class == TypeUnsignedInt || t.Class == TypeChar
}

func (t TypeSpec) IsNumeric() bool {
	return t.IsInteger() || t.Class == TypeFloat
}

func (t TypeSpec) SupportsAny() bool {
	return t.AnyKind != "" && t.AnyCtor != ""
}

var Types = []TypeSpec{
	{Name: "bool", Class: TypeBool, CName: "bool", AnyKind: "sealType_bool", AnyField: "as_bool", AnyCtor: "sealAny_bool"},

	// Seal int/uint are machine-word-sized. Fixed widths are available
	// through i8/i16/i32/i64 and u8/u16/u32/u64.
	{Name: "int", Class: TypeSignedInt, CName: "intptr_t", AnyKind: "sealType_int", AnyField: "as_int", AnyCtor: "sealAny_int"},
	{Name: "uint", Class: TypeUnsignedInt, CName: "uintptr_t", AnyKind: "sealType_uint", AnyField: "as_uint", AnyCtor: "sealAny_uint"},

	{Name: "i8", Class: TypeSignedInt, CName: "int8_t", AnyKind: "sealType_i8", AnyField: "as_i8", AnyCtor: "sealAny_i8"},
	{Name: "i16", Class: TypeSignedInt, CName: "int16_t", AnyKind: "sealType_i16", AnyField: "as_i16", AnyCtor: "sealAny_i16"},
	{Name: "i32", Class: TypeSignedInt, CName: "int32_t", AnyKind: "sealType_i32", AnyField: "as_i32", AnyCtor: "sealAny_i32"},
	{Name: "i64", Class: TypeSignedInt, CName: "int64_t", AnyKind: "sealType_i64", AnyField: "as_i64", AnyCtor: "sealAny_i64"},

	{Name: "u8", Class: TypeUnsignedInt, CName: "uint8_t", AnyKind: "sealType_u8", AnyField: "as_u8", AnyCtor: "sealAny_u8"},
	{Name: "u16", Class: TypeUnsignedInt, CName: "uint16_t", AnyKind: "sealType_u16", AnyField: "as_u16", AnyCtor: "sealAny_u16"},
	{Name: "u32", Class: TypeUnsignedInt, CName: "uint32_t", AnyKind: "sealType_u32", AnyField: "as_u32", AnyCtor: "sealAny_u32"},
	{Name: "u64", Class: TypeUnsignedInt, CName: "uint64_t", AnyKind: "sealType_u64", AnyField: "as_u64", AnyCtor: "sealAny_u64"},

	{Name: "f32", Class: TypeFloat, CName: "float", AnyKind: "sealType_f32", AnyField: "as_f32", AnyCtor: "sealAny_f32"},
	{Name: "f64", Class: TypeFloat, CName: "double", AnyKind: "sealType_f64", AnyField: "as_f64", AnyCtor: "sealAny_f64"},

	{Name: "char", Class: TypeChar, CName: "uint32_t", AnyKind: "sealType_char", AnyField: "as_char", AnyCtor: "sealAny_char"},

	{Name: "rawptr", Class: TypeRawptr, CName: "void *", AnyKind: "sealType_rawptr", AnyField: "as_rawptr", AnyCtor: "sealAny_rawptr"},
	{Name: "any", Class: TypeAny, CName: "sealAny", AnyKind: "sealType_any", AnyField: "", AnyCtor: "sealAny_any"},

	{Name: "string", Class: TypeString, CName: "sealString", AnyKind: "sealType_string", AnyField: "as_string", AnyCtor: "sealAny_string"},
	{Name: "cstring", Class: TypeCString, CName: "const char *", AnyKind: "sealType_cstring", AnyField: "as_cstring", AnyCtor: "sealAny_cstring"},
}

var TypeByName = func() map[string]TypeSpec {
	out := map[string]TypeSpec{}
	for _, typ := range Types {
		out[typ.Name] = typ
	}
	return out
}()

func LookupType(name string) (TypeSpec, bool) {
	typ, ok := TypeByName[name]
	return typ, ok
}

func IsType(name string) bool {
	_, ok := TypeByName[name]
	return ok
}

func AnyTypes() []TypeSpec {
	var out []TypeSpec
	for _, typ := range Types {
		if typ.SupportsAny() {
			out = append(out, typ)
		}
	}
	return out
}

type TaskKind int

const (
	TaskInvalid TaskKind = iota
	TaskAssert
	TaskLen
	TaskSize
	TaskAnyAs
	TaskAnyIs
	TaskPanic
	TaskTrap
	TaskUnreachable
	TaskCast
)

type TaskSpec struct {
	Name    string
	Kind    TaskKind
	Generic bool
}

var Tasks = []TaskSpec{
	{Name: "assert", Kind: TaskAssert},
	{Name: "len", Kind: TaskLen},
	{Name: "size", Kind: TaskSize},

	{Name: "anyAs", Kind: TaskAnyAs, Generic: true},
	{Name: "anyIs", Kind: TaskAnyIs, Generic: true},
	{Name: "cast", Kind: TaskCast, Generic: true},

	{Name: "panic", Kind: TaskPanic},
	{Name: "trap", Kind: TaskTrap},
	{Name: "unreachable", Kind: TaskUnreachable},
}

var TaskByName = func() map[string]TaskSpec {
	out := map[string]TaskSpec{}
	for _, task := range Tasks {
		out[task.Name] = task
	}
	return out
}()

func LookupTask(name string) (TaskSpec, bool) {
	task, ok := TaskByName[name]
	return task, ok
}

func IsTask(name string) bool {
	_, ok := TaskByName[name]
	return ok
}
