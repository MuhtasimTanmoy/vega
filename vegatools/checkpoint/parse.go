// Copyright (C) 2023 Gobalsky Labs Limited
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as
// published by the Free Software Foundation, either version 3 of the
// License, or (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>.

package checkpoint

import (
	"errors"
	"fmt"
	"log"
	"os"

	"code.vegaprotocol.io/vega/libs/proto"
	checkpoint "code.vegaprotocol.io/vega/protos/vega/checkpoint/v1"

	"google.golang.org/protobuf/reflect/protoreflect"
)

var (
	// ErrCheckpointFileEmpty obviously means the checkpoint file was empty.
	ErrCheckpointFileEmpty = errors.New("given checkpoint file is empty or unreadable")
	// ErrMissingOutFile no output file name argument provided.
	ErrMissingOutFile = errors.New("output file not specified")
)

// Run ... the main entry point of the command.
func Run(inFile, outFile string, generate, validate, dummy bool) error {
	if generate && outFile == "" {
		fmt.Println("No output file specified")
		return ErrMissingOutFile
	}
	// generate some files to play with
	if dummy {
		return generateDummy(inFile, outFile)
	}

	data, err := os.ReadFile(inFile)
	if err != nil {
		return err
	}

	fmt.Printf("Read %d bytes from %s\n", len(data), inFile)

	if len(data) == 0 {
		return ErrCheckpointFileEmpty
	}

	if generate {
		return generateCheckpoint(data, outFile)
	}

	cp := &checkpoint.Checkpoint{}
	if err = proto.Unmarshal(data, cp); err != nil {
		return err
	}

	parsed, err := unmarshalAll(cp)
	if err != nil {
		return err
	}
	// print output at the end
	defer func() {
		printParsed(parsed, err != nil)
	}()

	if validate {
		if err = parsed.CheckAssetsCollateral(); err != nil {
			return err
		}
	}

	return writeOut(parsed, outFile)
}

func generateDummy(cpF, jsonFName string) error {
	d := dummy()

	cp, h, err := d.CheckpointData() // get the data as checkpoint
	if err != nil {
		log.Printf("Could not convert dummy to checkpoint data to write to file: %+v\n", err)
		return err
	}

	if err = writeCheckpoint(cp, h, cpF); err != nil {
		log.Printf("Error writing checkpoint data to file '%s': %+v\n", cpF, err)
		return err
	}

	if err = writeOut(d, jsonFName); err != nil {
		log.Printf("Error writing JSON file '%s' from dummy: %+v\n", jsonFName, err)
		return err
	}
	return nil
}

func generateCheckpoint(data []byte, outF string) error {
	of, err := os.Create(outF)
	if err != nil {
		log.Printf("Failed to create output file %s: %+v\n", outF, err)
		return err
	}

	defer func() { _ = of.Close() }()

	a, err := fromJSON(data)
	if err != nil {
		log.Printf("Could not unmarshal input: %+v\n", err)
		return err
	}

	out, h, err := a.CheckpointData()
	if err != nil {
		log.Printf("Could not generate checkpoint data: %+v\n", err)
		return err
	}

	n, err := of.Write(out)
	if err != nil {
		log.Printf("Failed to write output to file: %+v\n", err)
		return err
	}

	fmt.Printf("Successfully wrote %d bytes to file %s\n", n, outF)
	fmt.Printf("hash for checkpoint is %s\n", h)
	return nil
}

func writeCheckpoint(data []byte, h string, outF string) error {
	of, err := os.Create(outF)
	if err != nil {
		fmt.Printf("Failed to create output file %s: %+v\n", outF, err)
		return err
	}

	defer func() { _ = of.Close() }()

	n, err := of.Write(data)
	if err != nil {
		fmt.Printf("Failed to write output to file '%s': %+v\n", outF, err)
		return err
	}

	fmt.Printf("Successfully wrote %d bytes to file %s\n", n, outF)
	fmt.Printf("Checkpoint hash is %s\n", h)
	return nil
}

func printParsed(a *all, isErr bool) {
	data, err := a.JSON()
	if err != nil {
		log.Printf("Failed to marshal data to JSON: %+v\n", err)
		return
	}

	if isErr {
		if _, err = os.Stderr.WriteString(string(data)); err == nil {
			return
		}
		fmt.Printf("Could not write to stderr: %+v\n", err)
	}

	fmt.Printf("Output:\n%s\n", string(data))
}

func writeOut(a *all, path string) error {
	if path == "" {
		return nil
	}

	data, err := a.JSON()
	if err != nil {
		return err
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}

	defer func() { _ = f.Close() }()

	n, err := f.Write(data)
	if err != nil {
		return err
	}

	fmt.Printf("Wrote %d bytes to %s\n", n, path)
	return nil
}

func unmarshalAll(cp *checkpoint.Checkpoint) (ret *all, err error) {
	ret = newAll()

	cp.ProtoReflect().Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		name := string(fd.Name())
		msg, ok := ret.messages[name]
		if ok {
			if err = proto.Unmarshal(v.Bytes(), msg); err != nil {
				return false
			}
		}
		return true
	})

	return ret, err
}
