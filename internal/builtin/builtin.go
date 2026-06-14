package builtin

type TypeClass int

const (
	TypeVoid TypeClass = iota
	TypeBool
	TypeSignedInt
	TypeUnsignedInt
	TypeFloat
	TypeChar
	TypeString
	TypeCString
	TypeRawptr
	TypeVoidptr
	TypeUintptr
	TypeAny
)

type TypeSpec struct {
	Name  string
	Class TypeClass
	CName string

	AnyKind  string
	AnyField string
	AnyCtor  string
}

var Types = []TypeSpec{
	{Name: "void", Class: TypeVoid, CName: "void"},

	{Name: "bool", Class: TypeBool, CName: "bool", AnyKind: "sealType_bool", AnyField: "as_bool", AnyCtor: "sealAny_bool"},

	{Name: "int", Class: TypeSignedInt, CName: "int", AnyKind: "sealType_int", AnyField: "as_int", AnyCtor: "sealAny_int"},
	{Name: "i8", Class: TypeSignedInt, CName: "int8_t", AnyKind: "sealType_i8", AnyField: "as_i8", AnyCtor: "sealAny_i8"},
	{Name: "i16", Class: TypeSignedInt, CName: "int16_t", AnyKind: "sealType_i16", AnyField: "as_i16", AnyCtor: "sealAny_i16"},
	{Name: "i32", Class: TypeSignedInt, CName: "int32_t", AnyKind: "sealType_i32", AnyField: "as_i32", AnyCtor: "sealAny_i32"},
	{Name: "i64", Class: TypeSignedInt, CName: "int64_t", AnyKind: "sealType_i64", AnyField: "as_i64", AnyCtor: "sealAny_i64"},
	{Name: "isize", Class: TypeSignedInt, CName: "ptrdiff_t", AnyKind: "sealType_isize", AnyField: "as_isize", AnyCtor: "sealAny_isize"},

	{Name: "u8", Class: TypeUnsignedInt, CName: "uint8_t", AnyKind: "sealType_u8", AnyField: "as_u8", AnyCtor: "sealAny_u8"},
	{Name: "u16", Class: TypeUnsignedInt, CName: "uint16_t", AnyKind: "sealType_u16", AnyField: "as_u16", AnyCtor: "sealAny_u16"},
	{Name: "u32", Class: TypeUnsignedInt, CName: "uint32_t", AnyKind: "sealType_u32", AnyField: "as_u32", AnyCtor: "sealAny_u32"},
	{Name: "u64", Class: TypeUnsignedInt, CName: "uint64_t", AnyKind: "sealType_u64", AnyField: "as_u64", AnyCtor: "sealAny_u64"},
	{Name: "usize", Class: TypeUnsignedInt, CName: "size_t", AnyKind: "sealType_usize", AnyField: "as_usize", AnyCtor: "sealAny_usize"},
	{Name: "uintptr", Class: TypeUintptr, CName: "uintptr_t", AnyKind: "sealType_uintptr", AnyField: "as_uintptr", AnyCtor: "sealAny_uintptr"},

	{Name: "f32", Class: TypeFloat, CName: "float", AnyKind: "sealType_f32", AnyField: "as_f32", AnyCtor: "sealAny_f32"},
	{Name: "f64", Class: TypeFloat, CName: "double", AnyKind: "sealType_f64", AnyField: "as_f64", AnyCtor: "sealAny_f64"},

	{Name: "char", Class: TypeChar, CName: "uint32_t", AnyKind: "sealType_char", AnyField: "as_char", AnyCtor: "sealAny_char"},

	{Name: "string", Class: TypeString, CName: "sealString", AnyKind: "sealType_string", AnyField: "as_string", AnyCtor: "sealAny_string"},
	{Name: "cstring", Class: TypeCString, CName: "const char *", AnyKind: "sealType_cstring", AnyField: "as_cstring", AnyCtor: "sealAny_cstring"},

	{Name: "rawptr", Class: TypeRawptr, CName: "void *", AnyKind: "sealType_rawptr", AnyField: "as_rawptr", AnyCtor: "sealAny_rawptr"},
	{Name: "voidptr", Class: TypeVoidptr, CName: "void *", AnyKind: "sealType_voidptr", AnyField: "as_voidptr", AnyCtor: "sealAny_voidptr"},

	{Name: "any", Class: TypeAny, CName: "sealAny", AnyKind: "sealType_any", AnyField: "", AnyCtor: "sealAny_any"},
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
