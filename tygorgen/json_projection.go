package tygorgen

import (
	"crypto/sha256"
	json "encoding/json/v2"
	"fmt"

	"tygor.dev/internal/jsoncontract"
	"tygor.dev/tygorgen/ir"
)

type jsonProjectionDirection string

const (
	jsonDecodeProjection jsonProjectionDirection = "Decode"
	jsonEncodeProjection jsonProjectionDirection = "Encode"
)

// jsonProjector materializes endpoint-specific wire declarations. A Go type
// may therefore have distinct request and response declarations when its
// effective encoding/json/v2 options differ by endpoint or direction.
type jsonProjector struct {
	schema      *ir.Schema
	projections map[string]ir.GoIdentifier
	processing  map[string]bool
}

func newJSONProjector(schema *ir.Schema) *jsonProjector {
	return &jsonProjector{
		schema:      schema,
		projections: make(map[string]ir.GoIdentifier),
		processing:  make(map[string]bool),
	}
}

func (p *jsonProjector) projectType(td ir.TypeDescriptor, direction jsonProjectionDirection, contract jsoncontract.Contract) (ir.TypeDescriptor, error) {
	if !jsonProjectionChangesShape(direction, contract) {
		return td, nil
	}
	return p.projectExpression(td, direction, contract)
}

func jsonProjectionChangesShape(direction jsonProjectionDirection, contract jsoncontract.Contract) bool {
	switch direction {
	case jsonDecodeProjection:
		return contract.Decode.StringifyNumbers
	case jsonEncodeProjection:
		return contract.Encode.StringifyNumbers || contract.Encode.FormatNilSliceAsNull ||
			contract.Encode.FormatNilMapAsNull || contract.Encode.OmitZeroStructFields
	default:
		return false
	}
}

func jsonProjectionKey(direction jsonProjectionDirection, contract jsoncontract.Contract) string {
	switch direction {
	case jsonDecodeProjection:
		return fmt.Sprintf("decode:stringify=%t", contract.Decode.StringifyNumbers)
	case jsonEncodeProjection:
		return fmt.Sprintf("encode:stringify=%t;nilSliceNull=%t;nilMapNull=%t;omitZero=%t",
			contract.Encode.StringifyNumbers, contract.Encode.FormatNilSliceAsNull,
			contract.Encode.FormatNilMapAsNull, contract.Encode.OmitZeroStructFields)
	default:
		panic("unknown JSON projection direction: " + direction)
	}
}

func (p *jsonProjector) projectExpression(td ir.TypeDescriptor, direction jsonProjectionDirection, contract jsoncontract.Contract) (ir.TypeDescriptor, error) {
	if td == nil {
		return nil, nil
	}

	switch d := td.(type) {
	case *ir.PrimitiveDescriptor:
		projected := *d
		if contractStringifiesNumbers(direction, contract) && isJSONNumberPrimitive(d.PrimitiveKind) {
			projected.StringEncoded = true
		}
		if direction == jsonEncodeProjection && d.PrimitiveKind == ir.PrimitiveBytes && d.ByteArrayLength == nil && contract.Encode.FormatNilSliceAsNull {
			return ir.Ptr(&projected), nil
		}
		return &projected, nil

	case *ir.ArrayDescriptor:
		element, err := p.projectExpression(d.Element, direction, contract)
		if err != nil {
			return nil, err
		}
		projected := &ir.ArrayDescriptor{Element: element, Length: d.Length, IsArray: d.IsArray}
		if direction == jsonEncodeProjection && d.IsSlice() && contract.Encode.FormatNilSliceAsNull {
			return ir.Ptr(projected), nil
		}
		return projected, nil

	case *ir.MapDescriptor:
		value, err := p.projectExpression(d.Value, direction, contract)
		if err != nil {
			return nil, err
		}
		// JSON object names are always strings. StringifyNumbers applies to
		// values, not the Go values used to derive map member names.
		projected := ir.Map(d.Key, value)
		if direction == jsonEncodeProjection && contract.Encode.FormatNilMapAsNull {
			return ir.Ptr(projected), nil
		}
		return projected, nil

	case *ir.PtrDescriptor:
		element, err := p.projectExpression(d.Element, direction, contract)
		if err != nil {
			return nil, err
		}
		return ir.Ptr(element), nil

	case *ir.ReferenceDescriptor:
		target, err := p.projectDeclaration(d.Target, direction, contract)
		if err != nil {
			return nil, err
		}
		arguments := make([]ir.TypeDescriptor, len(d.TypeArguments))
		for i, argument := range d.TypeArguments {
			arguments[i], err = p.projectExpression(argument, direction, contract)
			if err != nil {
				return nil, err
			}
		}
		return &ir.ReferenceDescriptor{Target: target, TypeArguments: arguments}, nil

	case *ir.UnionDescriptor:
		members := make([]ir.TypeDescriptor, len(d.Types))
		for i, member := range d.Types {
			var err error
			members[i], err = p.projectExpression(member, direction, contract)
			if err != nil {
				return nil, err
			}
		}
		return &ir.UnionDescriptor{Types: members}, nil

	case *ir.TypeParameterDescriptor:
		constraint, err := p.projectExpression(d.Constraint, direction, contract)
		if err != nil {
			return nil, err
		}
		return &ir.TypeParameterDescriptor{ParamName: d.ParamName, Constraint: constraint}, nil

	default:
		return nil, fmt.Errorf("project JSON contract for unsupported descriptor %T", td)
	}
}

func (p *jsonProjector) projectDeclaration(id ir.GoIdentifier, direction jsonProjectionDirection, contract jsoncontract.Contract) (ir.GoIdentifier, error) {
	key := id.Package + "\x00" + id.Name + "\x00" + string(direction) + "\x00" + jsonProjectionKey(direction, contract)
	if projected, ok := p.projections[key]; ok {
		return projected, nil
	}
	original := p.schema.FindType(id)
	if original == nil {
		return ir.GoIdentifier{}, fmt.Errorf("project JSON contract for unknown type %s.%s", id.Package, id.Name)
	}

	sum := sha256.Sum256([]byte(string(direction) + "\x00" + jsonProjectionKey(direction, contract)))
	projectedID := ir.GoIdentifier{
		Name:    fmt.Sprintf("%sJSON%s%x", id.Name, direction, sum[:4]),
		Package: id.Package,
	}
	p.projections[key] = projectedID
	if p.processing[key] {
		return projectedID, nil
	}
	p.processing[key] = true
	defer delete(p.processing, key)

	projected, err := p.cloneDeclaration(original, projectedID, direction, contract)
	if err != nil {
		return ir.GoIdentifier{}, err
	}
	p.schema.Types = append(p.schema.Types, projected)
	return projectedID, nil
}

func (p *jsonProjector) cloneDeclaration(original ir.TypeDescriptor, id ir.GoIdentifier, direction jsonProjectionDirection, contract jsoncontract.Contract) (ir.TypeDescriptor, error) {
	switch d := original.(type) {
	case *ir.StructDescriptor:
		fields := make([]ir.FieldDescriptor, len(d.Fields))
		for i, field := range d.Fields {
			projectedType, err := p.projectExpression(field.Type, direction, contract)
			if err != nil {
				return nil, fmt.Errorf("project field %s.%s: %w", d.Name.Name, field.Name, err)
			}
			fields[i] = field
			fields[i].Type = projectedType
			if direction == jsonEncodeProjection && contract.Encode.OmitZeroStructFields {
				fields[i].OmitZero = true
			}
			if contractStringifiesNumbers(direction, contract) {
				fields[i].StringEncoded = false
			}
		}
		extends := make([]ir.GoIdentifier, len(d.Extends))
		for i, extended := range d.Extends {
			var err error
			extends[i], err = p.projectDeclaration(extended, direction, contract)
			if err != nil {
				return nil, err
			}
		}
		parameters, err := p.projectTypeParameters(d.TypeParameters, direction, contract)
		if err != nil {
			return nil, err
		}
		return &ir.StructDescriptor{Name: id, TypeParameters: parameters, Fields: fields, Extends: extends, Documentation: d.Documentation, Source: d.Source}, nil

	case *ir.AliasDescriptor:
		underlying, err := p.projectExpression(d.Underlying, direction, contract)
		if err != nil {
			return nil, err
		}
		parameters, err := p.projectTypeParameters(d.TypeParameters, direction, contract)
		if err != nil {
			return nil, err
		}
		return &ir.AliasDescriptor{Name: id, TypeParameters: parameters, Underlying: underlying, Documentation: d.Documentation, Source: d.Source}, nil

	case *ir.EnumDescriptor:
		members := append([]ir.EnumMember(nil), d.Members...)
		var stringEncodedValues []string
		if contractStringifiesNumbers(direction, contract) && enumHasNumericValues(d) {
			stringEncodedValues = make([]string, len(members))
			for i := range members {
				switch members[i].Value.(type) {
				case int64, float64:
					value, err := stringifyEnumValue(members[i].Value, d.Underlying)
					if err != nil {
						return nil, fmt.Errorf("stringify enum %s member %s: %w", d.Name.Name, members[i].Name, err)
					}
					stringEncodedValues[i] = value
				}
			}
		}
		return &ir.EnumDescriptor{Name: id, Members: members, Underlying: d.Underlying, StringEncodedValues: stringEncodedValues, Documentation: d.Documentation, Source: d.Source}, nil

	default:
		return nil, fmt.Errorf("project JSON contract for unsupported declaration %T", original)
	}
}

func (p *jsonProjector) projectTypeParameters(parameters []ir.TypeParameterDescriptor, direction jsonProjectionDirection, contract jsoncontract.Contract) ([]ir.TypeParameterDescriptor, error) {
	projected := make([]ir.TypeParameterDescriptor, len(parameters))
	for i, parameter := range parameters {
		constraint, err := p.projectExpression(parameter.Constraint, direction, contract)
		if err != nil {
			return nil, err
		}
		projected[i] = ir.TypeParameterDescriptor{ParamName: parameter.ParamName, Constraint: constraint}
	}
	return projected, nil
}

func contractStringifiesNumbers(direction jsonProjectionDirection, contract jsoncontract.Contract) bool {
	if direction == jsonDecodeProjection {
		return contract.Decode.StringifyNumbers
	}
	return contract.Encode.StringifyNumbers
}

func isJSONNumberPrimitive(kind ir.PrimitiveKind) bool {
	return kind == ir.PrimitiveInt || kind == ir.PrimitiveUint || kind == ir.PrimitiveFloat
}

func enumHasNumericValues(enum *ir.EnumDescriptor) bool {
	if enum.Underlying != nil {
		return isJSONNumberPrimitive(enum.Underlying.PrimitiveKind)
	}
	if len(enum.Members) == 0 {
		return false
	}
	switch enum.Members[0].Value.(type) {
	case int64, float64:
		return true
	default:
		return false
	}
}

func stringifyEnumValue(value any, underlying *ir.PrimitiveDescriptor) (string, error) {
	wireValue := value
	if number, ok := value.(float64); ok && underlying != nil && underlying.PrimitiveKind == ir.PrimitiveFloat && underlying.BitSize == 32 {
		wireValue = float32(number)
	}
	data, err := json.Marshal(wireValue, json.StringifyNumbers(true))
	if err != nil {
		return "", err
	}
	var result string
	if err := json.Unmarshal(data, &result); err != nil {
		return "", err
	}
	return result, nil
}
