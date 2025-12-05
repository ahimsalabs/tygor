package provider

import (
	"context"
	"testing"

	"tygor.dev/tygorgen/ir"
)

func TestSourceProvider_GenericTypes(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("Response", "Container", "Pair"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	// Check Response[T any]
	responseType := findType(schema, "Response")
	if responseType == nil {
		t.Fatal("Response type not found")
	}

	responseStruct, ok := responseType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Response is not a StructDescriptor, got %T", responseType)
	}

	if len(responseStruct.TypeParameters) != 1 {
		t.Errorf("Response should have 1 type parameter, got %d", len(responseStruct.TypeParameters))
	} else {
		if responseStruct.TypeParameters[0].ParamName != "T" {
			t.Errorf("Type parameter should be named T, got %s", responseStruct.TypeParameters[0].ParamName)
		}
		if responseStruct.TypeParameters[0].Constraint != nil {
			t.Errorf("T should be unconstrained (nil), got %v", responseStruct.TypeParameters[0].Constraint)
		}
	}

	// Check Data field uses type parameter
	dataField := findFieldByName(responseStruct.Fields, "Data")
	if dataField == nil {
		t.Fatal("Data field not found")
	}
	if dataField.Type.Kind() != ir.KindTypeParameter {
		t.Errorf("Data field should be KindTypeParameter, got %v", dataField.Type.Kind())
	}
	typeParam := dataField.Type.(*ir.TypeParameterDescriptor)
	if typeParam.ParamName != "T" {
		t.Errorf("Data field type parameter should be T, got %s", typeParam.ParamName)
	}

	// Check Container[T comparable]
	containerType := findType(schema, "Container")
	if containerType == nil {
		t.Fatal("Container type not found")
	}

	containerStruct, ok := containerType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Container is not a StructDescriptor, got %T", containerType)
	}

	if len(containerStruct.TypeParameters) != 1 {
		t.Errorf("Container should have 1 type parameter, got %d", len(containerStruct.TypeParameters))
	} else {
		if containerStruct.TypeParameters[0].ParamName != "T" {
			t.Errorf("Type parameter should be named T, got %s", containerStruct.TypeParameters[0].ParamName)
		}
		// Note: comparable constraint is not preserved per spec (§3.4)
		if containerStruct.TypeParameters[0].Constraint != nil {
			t.Errorf("comparable constraint should be nil per spec, got %v", containerStruct.TypeParameters[0].Constraint)
		}
	}

	// Check Pair[K, V any]
	pairType := findType(schema, "Pair")
	if pairType == nil {
		t.Fatal("Pair type not found")
	}

	pairStruct, ok := pairType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("Pair is not a StructDescriptor, got %T", pairType)
	}

	if len(pairStruct.TypeParameters) != 2 {
		t.Errorf("Pair should have 2 type parameters, got %d", len(pairStruct.TypeParameters))
	} else {
		if pairStruct.TypeParameters[0].ParamName != "K" {
			t.Errorf("First type parameter should be K, got %s", pairStruct.TypeParameters[0].ParamName)
		}
		if pairStruct.TypeParameters[1].ParamName != "V" {
			t.Errorf("Second type parameter should be V, got %s", pairStruct.TypeParameters[1].ParamName)
		}
	}
}

func TestSourceProvider_RecursiveGeneric(t *testing.T) {
	provider := &SourceProvider{}
	schema, err := provider.BuildSchema(context.Background(), SourceInputOptions{
		Packages:  []string{"tygor.dev/tygorgen/provider/testdata"},
		RootTypes: rootTypes("TreeNode"),
	})

	if err != nil {
		t.Fatalf("BuildSchema failed: %v", err)
	}

	treeNodeType := findType(schema, "TreeNode")
	if treeNodeType == nil {
		t.Fatal("TreeNode type not found")
	}

	treeNodeStruct, ok := treeNodeType.(*ir.StructDescriptor)
	if !ok {
		t.Fatalf("TreeNode is not a StructDescriptor, got %T", treeNodeType)
	}

	// Check Children field
	childrenField := findFieldByName(treeNodeStruct.Fields, "Children")
	if childrenField == nil {
		t.Fatal("Children field not found")
	}

	// Should be an array
	if childrenField.Type.Kind() != ir.KindArray {
		t.Errorf("Children should be KindArray, got %v", childrenField.Type.Kind())
	}

	// The array element should be a reference to TreeNode
	arrayDesc := childrenField.Type.(*ir.ArrayDescriptor)
	if arrayDesc.Element.Kind() != ir.KindReference {
		t.Errorf("Children element should be KindReference, got %v", arrayDesc.Element.Kind())
	}
}
