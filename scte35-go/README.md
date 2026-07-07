# scte35-go: ANSI/SCTE 35 Decoder/Encoder

`scte35-go` is a Go library to supports creating, decorating, and analyzing
binary Digital Program Insertion Cueing Messages.

This library is fully compliant and compatible with all versions of the
[ANSI/SCTE 35](https://www.scte.org/standards-development/library/standards-catalog/scte-35-2019/)
specification up to and including [ANSI/SCTE 35 2022b](./docs/SCTE_35_2022b.pdf).

This project uses [Semantic Versioning](https://semver.org) and is published as
a [Go Module](https://blog.golang.org/using-go-modules).

[![Build Status](https://github.com/Comcast/scte35-go/actions/workflows/check.yml/badge.svg)](https://github.com/Comcast/scte35-go/actions/workflows/check.yml)
[![PkgGoDev](https://pkg.go.dev/badge/github.com/Comcast/scte35-go)](https://pkg.go.dev/github.com/Comcast/scte35-go)

## Getting Started

Get the module:

```shell
$ go get github.com/Comcast/scte35-go
go get: added github.com/Comcast/scte35-go v1.2.1
```

## Code Examples

Additional examples can be found in [examples](./examples).

#### Decode Signal

Binary signals can be quickly and easily decoded from base-64 or hexadecimal
strings.

The results can be output as a:
* String - emulating the table structure used in the [SCTE 35 specification](./docs/SCTE_35_2022b.pdf).
* XML - compliant with the [SCTE 35 XML Schema](./docs/scte_35_20220816.xsd)
* JSON - for integrating with JSON based tools such as [jq](https://stedolan.github.io/jq/)

[examples/simple_decode/main.go](examples/simple_decode/main.go)

```go
package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"os"

	"github.com/Comcast/scte35-go/pkg/scte35"
)

func main() {
	sis, _ := scte35.DecodeBase64("/DA8AAAAAAAAAP///wb+06ACpQAmAiRDVUVJAACcHX//AACky4AMEERJU0NZTVdGMDQ1MjAwMEgxAQEMm4c0")

	// details
	_, _ = fmt.Fprintf(os.Stdout, "\nTable: \n%s\n", sis.Table("", "\t"))

	// xml
	b, _ := xml.MarshalIndent(sis, "", "\t")
	_, _ = fmt.Fprintf(os.Stdout, "\nXML: \n%s\n", b)

	// json
	b, _ = json.MarshalIndent(sis, "", "\t")
	_, _ = fmt.Fprintf(os.Stdout, "\nJSON: \n%s\n", b)
}
```

```shell
$ go run examples/simple_decode/main.go

Table: 
splice_info_section() {
	table_id: 0xfc
	section_syntax_indicator: false
	private_indicator: false
	sap_type: 3 (Not Specified)
	section_length: 60
}
protocol_version: 0
encryption_algorithm: 0 (No encryption)
pts_adjustment: 0
cw_index: 0
tier: 4095
splice_command_length: 5
splice_command_type: 0x06
time_signal() {
	time_specified_flag: true
	pts_time: 3550479013
}
descriptor_loop_length: 38
segmentation_descriptor() {
	splice_descriptor_tag: 0x02
	descriptor_length: 36
	identifier: 0x43554549 (CUEI)
	segmentation_event_id: 39965
	segmentation_event_cancel_indicator: false
	segmentation_event_id_compliance_indicator: false
	program_segmentation_flag: true
	segmentation_duration_flag: true
	delivery_not_restricted_flag: true
	segmentation_duration: 10800000
	segmentation_upid_length: 16
	segmentation_upid[0] {
		segmentation_upid_type: 0x0c (MPU())
		format_identifier: DISC
		segmentation_upid: WU1XRjA0NTIwMDBI
	}
	segmentation_type_id: 0x31 (Provider Advertisement End)
	segment_num: 1
	segments_expected: 1
}


XML: 
<SpliceInfoSection xmlns="http://www.scte.org/schemas/35" sapType="3" tier="4095">
	<EncryptedPacket xmlns="http://www.scte.org/schemas/35" encryptionAlgorithm="0" cwIndex="0"></EncryptedPacket>
	<TimeSignal xmlns="http://www.scte.org/schemas/35">
		<SpliceTime xmlns="http://www.scte.org/schemas/35" ptsTime="3550479013"></SpliceTime>
	</TimeSignal>
	<SegmentationDescriptor xmlns="http://www.scte.org/schemas/35" segmentationEventId="39965" segmentationDuration="10800000" segmentationTypeId="49" segmentNum="1" segmentsExpected="1">
		<SegmentationUpid xmlns="http://www.scte.org/schemas/35" segmentationUpidType="12" segmentationUpidFormat="base-64" formatIdentifier="1145656131">WU1XRjA0NTIwMDBI</SegmentationUpid>
	</SegmentationDescriptor>
</SpliceInfoSection>

JSON: 
{
	"encryptedPacket": {
		"encryptionAlgorithm": 0,
		"cwIndex": 0
	},
	"sapType": 3,
	"spliceCommand": {
		"type": 6,
		"spliceTime": {
			"ptsTime": 3550479013
		}
	},
	"spliceDescriptors": [
		{
			"type": 2,
			"segmentationUpids": [
				{
					"segmentationUpidType": 12,
					"segmentationUpidFormat": "base-64",
					"formatIdentifier": 1145656131,
					"value": "WU1XRjA0NTIwMDBI"
				}
			],
			"segmentationEventId": 39965,
			"segmentationDuration": 10800000,
			"segmentationTypeId": 49,
			"segmentNum": 1,
			"segmentsExpected": 1
		}
	],
	"tier": 4095
}
```

#### Encode Signal

Encoding signals is equally simple. You can start from scratch and build a
`scte35.SpliceInfoSection` or decode an existing signal and modify it to suit
your needs.

[examples/simple_encode/main.go](examples/simple_encode/main.go)

```go
package main

import (
	"fmt"
	"os"

	"github.com/Comcast/scte35-go/pkg/scte35"
)

func main() {
	// start with a signal
	sis := scte35.SpliceInfoSection{
		SpliceCommand: scte35.NewTimeSignal(0x072bd0050),
		SpliceDescriptors: []scte35.SpliceDescriptor{
			&scte35.SegmentationDescriptor{
				DeliveryRestrictions: &scte35.DeliveryRestrictions{
					NoRegionalBlackoutFlag: true,
					ArchiveAllowedFlag:     true,
					DeviceRestrictions:     scte35.DeviceRestrictionsNone,
				},
				SegmentationEventID: uint32(0x4800008e),
				SegmentationTypeID:  scte35.SegmentationTypeProviderPOStart,
				SegmentationUPIDs: []scte35.SegmentationUPID{
					scte35.NewSegmentationUPID(scte35.SegmentationUPIDTypeTI, []byte("78511452")),
				},
				SegmentNum: 2,
			},
		},
		EncryptedPacket: scte35.EncryptedPacket{
			EncryptionAlgorithm: scte35.EncryptionAlgorithmNone,
			CWIndex:             255,
		},
		Tier:    4095,
		SAPType: 3,
	}

	// encode it
	_, _ = fmt.Fprintf(os.Stdout, "Original:\n")
	_, _ = fmt.Fprintf(os.Stdout, "base-64: %s\n", sis.Base64())
	_, _ = fmt.Fprintf(os.Stdout, "hex    : %s\n", sis.Hex())

	// add a segmentation descriptor
	sis.SpliceDescriptors = append(
		sis.SpliceDescriptors,
		&scte35.DTMFDescriptor{
			DTMFChars: "ABC*",
		},
	)

	// encode it again
	_, _ = fmt.Fprintf(os.Stdout, "Original:\n")
	_, _ = fmt.Fprintf(os.Stdout, "base-64: %s\n", sis.Base64())
	_, _ = fmt.Fprintf(os.Stdout, "hex    : %s\n", sis.Hex())
}
```

```shell
$ go run examples/simple_encode/main.go
Original:
base-64: /DAvAAAAAAAA///wBQb+cr0AUAAZAhdDVUVJSAAAjn+PCAg3ODUxMTQ1MjQCADhqB9E=
hex    : fc302f000000000000fffff00506fe72bd005000190217435545494800008e7f8f08083738353131343532340200386a07d1
Original:
base-64: /DA7AAAAAAAA///wBQb+cr0AUAAlAhdDVUVJSAAAjn+PCAg3ODUxMTQ1MjQCAAEKQ1VFSQCfQUJDKqtwQlQ=
hex    : fc303b000000000000fffff00506fe72bd005000250217435545494800008e7f8f08083738353131343532340200010a43554549009f4142432aab704254
```

#### Decoding Non-Compliant Signals

The SCTE 35 decoder will always return a non-nil `SpliceInfoSection`, even when
an error occurs. This is done to help better identify the specific cause of the
decoding failure.

[examples/bad_signal/main.go](examples/bad_signal/main.go)

```go
package main

import (
	"fmt"
	"os"

	"github.com/Comcast/scte35-go/pkg/scte35"
)

func main() {
	sis, err := scte35.DecodeBase64("FkC1lwP3uTQD0VvxHwVBEH89G6B7VjzaZ9eNuyUF9q8pYAIXsRM9ZpDCczBeDbytQhXkssQstGJVGcvjZ3tiIMULiA4BpRHlzLGFa0q6aVMtzk8ZRUeLcxtKibgVOKBBnkCbOQyhSflFiDkrAAIp+Fk+VRsByTSkPN3RvyK+lWcjHElhwa9hNFcAy4dm3DdeRXnrD3I2mISNc7DkgS0ReotPyp94FV77xMHT4D7SYL48XU20UM4bgg==")
	if err != nil {
		_, _ = fmt.Fprintf(os.Stdout, "Error: %s\n", err)
	}
	_, _ = fmt.Fprintf(os.Stdout, "%s\n", sis.Table("", "\t"))
}
```

As we can see from the output below, the signal has a corrupted `component_count`,
causing the decoder to return a `scte.ErrBufferOverflow`:

```shell
$ go run examples/bad_signal/main.go
Error: splice_insert: buffer overflow
splice_info_section() {
	table_id: 0xfc
	section_syntax_indicator: false
	private_indicator: false
	sap_type: 0 (Type 1)
	section_length: 347
}
protocol_version: 151
encryption_algorithm: 1 (DES – ECB mode)
pts_adjustment: 8451077123
cw_index: 209
tier: 1471
splice_command_length: 326
splice_command_type: 0x05
splice_insert() {
	splice_event_id: 1091600189
	splice_event_cancel_indicator: false
	out_of_network_indicator: true
	program_splice_flag: false
	duration_flag: true
	splice_immediate_flag: false
	component_count: 123
	component[0]
		component_tag: 86
		time_specified_flag: false
    }

    ... additional components removed

        auto_return: false
        duration: 0
        unique_program_id: 0
        avail_num: 0
        avails_expected: 0
}
descriptor_loop_length: 0
```

#### CRC_32 Validation

The SCTE 35 decoder performs automatic `CRC_32` validation. The returned error
can be explicitly ignored if desired.

[examples/ignore_crc32/main.go](examples/ignore_crc32/main.go)

```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/Comcast/scte35-go/pkg/scte35"
)

func main() {
	scte35.Logger.SetOutput(os.Stdout)

	sis, err := scte35.DecodeBase64("/DA4AAAAAAAAAP/wFAUABDEAf+//mWEhzP4Azf5gAQAAAAATAhFDVUVJAAAAAX+/AQIwNAEAAKeYO3Q=")
	if errors.Is(err, scte35.ErrCRC32Invalid) {
		_, _ = fmt.Fprintf(os.Stderr, "Warning: CRC32 check failed!\n")
	}
	_, _ = fmt.Fprintf(os.Stdout, "%s\n", sis.Table("", "\t"))
}
```

```shell
$ go run examples/ignore_crc32/main.go
2025/03/15 00:44:42 CRC_32 calculated (2811771763) != reported (2811771764)
Warning: CRC32 check failed!
splice_info_section() {
	table_id: 0xfc

... more output removed ...
```

#### Logging

Additional diagnostics can be enabled by redirecting the output of
`scte35.Logger`

```go
scte35.Logger.SetOutput(os.Stderr)
```

## Command Line Interface

This package also provides a simple command line interface that supports
encoding and decoding signals from the command line.

```shell
$ ./scte35-go --help
SCTE-35 CLI

Usage:
  scte35-go [command]

Available Commands:
  decode      Decode a splice_info_section from binary
  encode      Encode a splice_info_section to binary
  help        Help about any command

Flags:
  -h, --help   help for scte35-go
```

## License

`scte35-go` is licensed under [Apache License 2.0](/LICENSE.md).

## Code of Conduct

We take our [code of conduct](CODE_OF_CONDUCT.md) very seriously. Please abide
by it.

## Contributing

Please read our [contributing guide](CONTRIBUTING.md) for details on how to
contribute to our project.
