package lsp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"
	"sync"
)

const defaultMaximumMessageSize int64 = 64 * 1024 * 1024

/*
Transport implements LSP's Content-Length framed JSON-RPC transport.
*/
type Transport struct {
	reader *bufio.Reader
	writer io.Writer

	writeMu sync.Mutex

	maximumMessageSize int64
}

func NewTransport(
	reader io.Reader,
	writer io.Writer,
) *Transport {
	return &Transport{
		reader: bufio.NewReader(
			reader,
		),

		writer: writer,

		maximumMessageSize: defaultMaximumMessageSize,
	}
}

func (t *Transport) SetMaximumMessageSize(
	size int64,
) {
	if t == nil ||
		size <= 0 {
		return
	}

	t.maximumMessageSize =
		size
}

func (t *Transport) ReadMessage() (
	[]byte,
	error,
) {
	if t == nil ||
		t.reader == nil {
		return nil,
			fmt.Errorf(
				"missing LSP transport reader",
			)
	}

	contentLength := int64(-1)

	for {
		line, err :=
			t.reader.ReadString(
				'\n',
			)

		if err != nil {
			if err == io.EOF &&
				len(line) == 0 {
				return nil,
					io.EOF
			}

			return nil,
				fmt.Errorf(
					"reading LSP header: %w",
					err,
				)
		}

		line =
			strings.TrimRight(
				line,
				"\r\n",
			)

		if line == "" {
			break
		}

		name, value, found :=
			strings.Cut(
				line,
				":",
			)

		if !found {
			return nil,
				fmt.Errorf(
					"invalid LSP header %q",
					line,
				)
		}

		name =
			strings.TrimSpace(
				name,
			)

		value =
			strings.TrimSpace(
				value,
			)

		switch strings.ToLower(name) {
		case "content-length":
			parsedLength, err :=
				strconv.ParseInt(
					value,
					10,
					64,
				)

			if err != nil {
				return nil,
					fmt.Errorf(
						"invalid Content-Length %q: %w",
						value,
						err,
					)
			}

			if parsedLength < 0 {
				return nil,
					fmt.Errorf(
						"negative Content-Length %d",
						parsedLength,
					)
			}

			contentLength =
				parsedLength

		default:
			/*
				Content-Type and unknown extension headers are ignored.
			*/
		}
	}

	if contentLength < 0 {
		return nil,
			fmt.Errorf(
				"LSP message has no Content-Length header",
			)
	}

	if contentLength >
		t.maximumMessageSize {
		return nil,
			fmt.Errorf(
				"LSP message is %d bytes; maximum is %d",
				contentLength,
				t.maximumMessageSize,
			)
	}

	payload :=
		make(
			[]byte,
			contentLength,
		)

	if _, err :=
		io.ReadFull(
			t.reader,
			payload,
		); err != nil {
		return nil,
			fmt.Errorf(
				"reading LSP payload: %w",
				err,
			)
	}

	return payload,
		nil
}

func (t *Transport) WriteMessage(
	message any,
) error {
	if t == nil ||
		t.writer == nil {
		return fmt.Errorf(
			"missing LSP transport writer",
		)
	}

	payload, err :=
		json.Marshal(
			message,
		)

	if err != nil {
		return fmt.Errorf(
			"encoding LSP message: %w",
			err,
		)
	}

	if int64(len(payload)) >
		t.maximumMessageSize {
		return fmt.Errorf(
			"LSP response is %d bytes; maximum is %d",
			len(payload),
			t.maximumMessageSize,
		)
	}

	header :=
		fmt.Sprintf(
			"Content-Length: %d\r\n\r\n",
			len(payload),
		)

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if err :=
		writeAll(
			t.writer,
			[]byte(header),
		); err != nil {
		return fmt.Errorf(
			"writing LSP header: %w",
			err,
		)
	}

	if err :=
		writeAll(
			t.writer,
			payload,
		); err != nil {
		return fmt.Errorf(
			"writing LSP payload: %w",
			err,
		)
	}

	return nil
}

func writeAll(
	writer io.Writer,
	data []byte,
) error {
	for len(data) > 0 {
		written, err :=
			writer.Write(data)

		if err != nil {
			return err
		}

		if written <= 0 {
			return io.ErrShortWrite
		}

		data =
			data[written:]
	}

	return nil
}
