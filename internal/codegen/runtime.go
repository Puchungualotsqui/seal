package cgen

import (
	"fmt"
	"seal/internal/ast"
	"seal/internal/builtin"
	"seal/internal/token"
	"sort"
	"strings"
)

func cExternDeclaredByRuntimeHeaders(
	info TaskInfo,
) bool {
	if !info.IsExtern {
		return false
	}

	name := strings.TrimSpace(
		info.ExternName,
	)

	switch name {
	// <stdlib.h>
	case "malloc",
		"calloc",
		"realloc",
		"free",
		"abort":
		return true

	// <string.h>
	case "memcpy",
		"memmove",
		"memcmp",
		"memset",
		"strlen",
		"strcmp",
		"strncmp",
		"strcpy",
		"strncpy":
		return true

	// <stdio.h>
	case "printf",
		"fprintf",
		"sprintf",
		"snprintf",
		"puts",
		"fputs",
		"fwrite",
		"fputc":
		return true
	}

	return false
}

func (g *Generator) emitAnyRuntimeSupport() {
	anyTypes := builtin.AnyTypes()

	g.line("typedef enum sealTypeKind {")
	g.indent++
	g.line("sealType_invalid = 0,")

	for i, typ := range anyTypes {
		comma := ","
		if i == len(anyTypes)-1 {
			comma = ""
		}

		g.linef("%s%s", typ.AnyKind, comma)
	}

	g.indent--
	g.line("} sealTypeKind;")
	g.line("")

	g.line("typedef struct sealAny {")
	g.indent++
	g.line("sealTypeKind type;")
	g.line("union {")
	g.indent++

	for _, typ := range anyTypes {
		if typ.Name == "any" {
			continue
		}

		if typ.AnyField == "" {
			continue
		}

		g.linef("%s %s;", typ.CName, typ.AnyField)
	}

	g.indent--
	g.line("} value;")
	g.indent--
	g.line("} sealAny;")
	g.line("")

	for _, typ := range anyTypes {
		if typ.Name == "any" {
			g.line("static inline sealAny sealAny_any(sealAny value) { return value; }")
			continue
		}

		if typ.AnyField == "" {
			continue
		}

		g.linef(
			"static inline sealAny %s(%s value) { sealAny out; out.type = %s; out.value.%s = value; return out; }",
			typ.AnyCtor,
			typ.CName,
			typ.AnyKind,
			typ.AnyField,
		)
	}

	g.line("")
}

func (g *Generator) emitRuntimeSupport() {
	g.line("typedef struct sealString {")
	g.indent++
	g.line("const uint8_t *data;")
	g.line("uintptr_t len;")
	g.indent--
	g.line("} sealString;")
	g.line("")

	g.emitAnyRuntimeSupport()

	g.line("static inline void seal_trap(void) {")
	g.indent++
	g.line("#if defined(__GNUC__) || defined(__clang__)")
	g.line("__builtin_trap();")
	g.line("#else")
	g.line("abort();")
	g.line("#endif")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_unreachable(void) {")
	g.indent++
	g.line("#if defined(__GNUC__) || defined(__clang__)")
	g.line("__builtin_unreachable();")
	g.line("#else")
	g.line("abort();")
	g.line("#endif")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_empty(void) {")
	g.indent++
	g.line(`fprintf(stderr, "panic\n");`)
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_cstring(const char *message) {")
	g.indent++
	g.line(`fprintf(stderr, "panic: %s\n", message ? message : "<null>");`)
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline void seal_panic_string(sealString message) {")
	g.indent++
	g.line(`fputs("panic: ", stderr);`)
	g.line("if (message.len != 0) {")
	g.indent++
	g.line("if (message.data == NULL) {")
	g.indent++
	g.line(`fputs("<invalid string>", stderr);`)
	g.indent--
	g.line("} else {")
	g.indent++
	g.line("fwrite(message.data, 1, (size_t)message.len, stderr);")
	g.indent--
	g.line("}")
	g.indent--
	g.line("}")
	g.line(`fputc('\n', stderr);`)
	g.line("abort();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_utf8_fail(void) {")
	g.indent++
	g.line(`seal_panic_cstring("invalid UTF-8 or string index out of bounds");`)
	g.line("return 0;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_cstring_byte_len(const char *value) {")
	g.indent++
	g.line("if (value == NULL) {")
	g.indent++
	g.line("return 0;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("const uint8_t *bytes = (const uint8_t *)value;")
	g.line("uintptr_t len = 0;")
	g.line("")
	g.line("while (bytes[len] != 0) {")
	g.indent++
	g.line("len++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return len;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline const char *seal_cstring_from_parts(")
	g.indent++
	g.line("const char *data,")
	g.line("uintptr_t byte_len")
	g.indent--
	g.line(") {")
	g.indent++

	g.line("if (data == NULL) {")
	g.indent++
	g.line(`seal_panic_cstring("cannot construct cstring from a null pointer");`)
	g.line("return NULL;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (((const uint8_t *)data)[byte_len] != 0) {")
	g.indent++
	g.line(`seal_panic_cstring("cstring data is not null-terminated at the supplied byte length");`)
	g.line("return NULL;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("return data;")

	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline bool seal_utf8_is_continuation(uint8_t byte) {")
	g.indent++
	g.line("return (byte & 0xC0u) == 0x80u;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_utf8_decode_one(")
	g.indent++
	g.line("const uint8_t *data,")
	g.line("uintptr_t byte_len,")
	g.line("uintptr_t *offset")
	g.indent--
	g.line(") {")
	g.indent++

	g.line("if (data == NULL || offset == NULL || *offset >= byte_len) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("uintptr_t i = *offset;")
	g.line("uint8_t b0 = data[i];")
	g.line("")

	g.line("if (b0 <= 0x7Fu) {")
	g.indent++
	g.line("*offset = i + 1;")
	g.line("return (uint32_t)b0;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (b0 >= 0xC2u && b0 <= 0xDFu) {")
	g.indent++
	g.line("if (byte_len - i < 2) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("uint8_t b1 = data[i + 1];")
	g.line("")
	g.line("if (!seal_utf8_is_continuation(b1)) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("*offset = i + 2;")
	g.line("")
	g.line("return ((uint32_t)(b0 & 0x1Fu) << 6) |")
	g.indent++
	g.line("(uint32_t)(b1 & 0x3Fu);")
	g.indent--
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (b0 >= 0xE0u && b0 <= 0xEFu) {")
	g.indent++
	g.line("if (byte_len - i < 3) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("uint8_t b1 = data[i + 1];")
	g.line("uint8_t b2 = data[i + 2];")
	g.line("")
	g.line("if (!seal_utf8_is_continuation(b1) ||")
	g.indent++
	g.line("!seal_utf8_is_continuation(b2)) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.indent--
	g.line("")
	g.line("if (b0 == 0xE0u && b1 < 0xA0u) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (b0 == 0xEDu && b1 > 0x9Fu) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("*offset = i + 3;")
	g.line("")
	g.line("return ((uint32_t)(b0 & 0x0Fu) << 12) |")
	g.indent++
	g.line("((uint32_t)(b1 & 0x3Fu) << 6) |")
	g.line("(uint32_t)(b2 & 0x3Fu);")
	g.indent--
	g.indent--
	g.line("}")
	g.line("")

	g.line("if (b0 >= 0xF0u && b0 <= 0xF4u) {")
	g.indent++
	g.line("if (byte_len - i < 4) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("uint8_t b1 = data[i + 1];")
	g.line("uint8_t b2 = data[i + 2];")
	g.line("uint8_t b3 = data[i + 3];")
	g.line("")
	g.line("if (!seal_utf8_is_continuation(b1) ||")
	g.indent++
	g.line("!seal_utf8_is_continuation(b2) ||")
	g.line("!seal_utf8_is_continuation(b3)) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.indent--
	g.line("")
	g.line("if (b0 == 0xF0u && b1 < 0x90u) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (b0 == 0xF4u && b1 > 0x8Fu) {")
	g.indent++
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("*offset = i + 4;")
	g.line("")
	g.line("return ((uint32_t)(b0 & 0x07u) << 18) |")
	g.indent++
	g.line("((uint32_t)(b1 & 0x3Fu) << 12) |")
	g.line("((uint32_t)(b2 & 0x3Fu) << 6) |")
	g.line("(uint32_t)(b3 & 0x3Fu);")
	g.indent--
	g.indent--
	g.line("}")
	g.line("")

	g.line("return seal_utf8_fail();")

	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_utf8_scalar_count(")
	g.indent++
	g.line("const uint8_t *data,")
	g.line("uintptr_t byte_len")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("uintptr_t offset = 0;")
	g.line("uintptr_t count = 0;")
	g.line("")
	g.line("while (offset < byte_len) {")
	g.indent++
	g.line("(void)seal_utf8_decode_one(data, byte_len, &offset);")
	g.line("count++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return count;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_string_scalar_len(sealString value) {")
	g.indent++
	g.line("return seal_utf8_scalar_count(value.data, value.len);")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uintptr_t seal_cstring_scalar_len(const char *value) {")
	g.indent++
	g.line("uintptr_t byte_len = seal_cstring_byte_len(value);")
	g.line("")
	g.line("return seal_utf8_scalar_count(")
	g.indent++
	g.line("(const uint8_t *)value,")
	g.line("byte_len")
	g.indent--
	g.line(");")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline intptr_t seal_utf8_normalize_index(")
	g.indent++
	g.line("intptr_t index,")
	g.line("uintptr_t length")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("if (length > (uintptr_t)INTPTR_MAX) {")
	g.indent++
	g.line("return (intptr_t)seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")
	g.line("intptr_t normalized = index;")
	g.line("")
	g.line("if (normalized < 0) {")
	g.indent++
	g.line("normalized += (intptr_t)length;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (normalized < 0 ||")
	g.indent++
	g.line("(uintptr_t)normalized >= length) {")
	g.indent++
	g.line("return (intptr_t)seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.indent--
	g.line("")
	g.line("return normalized;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_string_index(")
	g.indent++
	g.line("sealString value,")
	g.line("intptr_t index")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("uintptr_t scalar_len = seal_string_scalar_len(value);")
	g.line("intptr_t normalized =")
	g.indent++
	g.line("seal_utf8_normalize_index(index, scalar_len);")
	g.indent--
	g.line("")
	g.line("uintptr_t offset = 0;")
	g.line("uintptr_t current = 0;")
	g.line("")
	g.line("while (offset < value.len) {")
	g.indent++
	g.line("uint32_t scalar = seal_utf8_decode_one(")
	g.indent++
	g.line("value.data,")
	g.line("value.len,")
	g.line("&offset")
	g.indent--
	g.line(");")
	g.line("")
	g.line("if (current == (uintptr_t)normalized) {")
	g.indent++
	g.line("return scalar;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("current++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline uint32_t seal_cstring_index(")
	g.indent++
	g.line("const char *value,")
	g.line("intptr_t index")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("uintptr_t byte_len = seal_cstring_byte_len(value);")
	g.line("uintptr_t scalar_len = seal_utf8_scalar_count(")
	g.indent++
	g.line("(const uint8_t *)value,")
	g.line("byte_len")
	g.indent--
	g.line(");")
	g.line("")
	g.line("intptr_t normalized =")
	g.indent++
	g.line("seal_utf8_normalize_index(index, scalar_len);")
	g.indent--
	g.line("")
	g.line("uintptr_t offset = 0;")
	g.line("uintptr_t current = 0;")
	g.line("")
	g.line("while (offset < byte_len) {")
	g.indent++
	g.line("uint32_t scalar = seal_utf8_decode_one(")
	g.indent++
	g.line("(const uint8_t *)value,")
	g.line("byte_len,")
	g.line("&offset")
	g.indent--
	g.line(");")
	g.line("")
	g.line("if (current == (uintptr_t)normalized) {")
	g.indent++
	g.line("return scalar;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("current++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return seal_utf8_fail();")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline bool seal_string_equal(")
	g.indent++
	g.line("sealString left,")
	g.line("sealString right")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("if (left.len != right.len) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (left.len == 0) {")
	g.indent++
	g.line("return true;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (left.data == NULL || right.data == NULL) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("for (uintptr_t i = 0; i < left.len; i++) {")
	g.indent++
	g.line("if (left.data[i] != right.data[i]) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return true;")
	g.indent--
	g.line("}")
	g.line("")

	g.line("static inline bool seal_cstring_equal(")
	g.indent++
	g.line("const char *left,")
	g.line("const char *right")
	g.indent--
	g.line(") {")
	g.indent++
	g.line("if (left == right) {")
	g.indent++
	g.line("return true;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("if (left == NULL || right == NULL) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("const uint8_t *a = (const uint8_t *)left;")
	g.line("const uint8_t *b = (const uint8_t *)right;")
	g.line("uintptr_t i = 0;")
	g.line("")
	g.line("while (a[i] != 0 && b[i] != 0) {")
	g.indent++
	g.line("if (a[i] != b[i]) {")
	g.indent++
	g.line("return false;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("i++;")
	g.indent--
	g.line("}")
	g.line("")
	g.line("return a[i] == b[i];")
	g.indent--
	g.line("}")
	g.line("")

	for _, typ := range builtin.Types {
		cType := CType{
			Name:     typ.CName,
			SealName: typ.Name,
		}

		g.emitVariadicRuntimeType(cType)
	}

	g.line("")
}

func (g *Generator) emitTaskVariadicRuntimeTypes() {
	names := make([]string, 0, len(g.tasks))
	for name := range g.tasks {
		names = append(names, name)
	}
	sort.Strings(names)

	for _, name := range names {
		info := g.tasks[name]

		for i, paramType := range info.ParamTypes {
			if i < len(info.ParamIsVariadic) && info.ParamIsVariadic[i] {
				g.emitVariadicRuntimeType(paramType)
			}
		}
	}

	if len(names) > 0 {
		g.line("")
	}
}

func (g *Generator) emitVariadicRuntimeType(
	elem CType,
) {
	variadicType := g.variadicCType(elem)
	name := variadicType.Name

	if g.emittedVariadics[name] {
		return
	}

	g.emittedVariadics[name] = true

	g.linef("typedef struct %s {", name)
	g.indent++
	g.linef("%s *data;", elem.Name)
	g.line("size_t len;")
	g.indent--
	g.linef("} %s;", name)
}

func (g *Generator) variadicCType(elem CType) CType {
	elemName := elem.SealName

	name := "sealVariadic_" + sanitizeCName(elemName)

	return CType{
		Name:       name,
		SealName:   "..." + elem.SealName,
		IsVariadic: true,
		Elem:       &elem,
	}
}

func (g *Generator) emitCImports(file *ast.File) {
	emitted := false

	for _, decl := range file.Decls {
		d, ok := decl.(*ast.DirectiveDecl)
		if !ok {
			continue
		}

		if d.Directive.Name != "c_import" {
			continue
		}

		for i := 0; i < len(d.Body); i++ {
			tok := d.Body[i]

			if tok.Kind == token.Ident && tok.Lexeme == "include" {
				if i+1 >= len(d.Body) || d.Body[i+1].Kind != token.StringLit {
					g.error(tok.Span, "expected string literal after include in @c_import")
					continue
				}

				g.linef("#include %s", d.Body[i+1].Lexeme)
				emitted = true
				i++
				continue
			}

			if tok.Kind == token.Ident {
				g.error(tok.Span, fmt.Sprintf("unsupported @c_import directive item %q", tok.Lexeme))
			}
		}
	}

	if emitted {
		g.line("")
	}
}
