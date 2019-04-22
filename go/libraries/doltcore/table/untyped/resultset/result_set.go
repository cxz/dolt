package resultset

import (
	"errors"
	"fmt"
	"github.com/attic-labs/noms/go/types"
	"github.com/liquidata-inc/ld/dolt/go/libraries/doltcore/rowconv"
	"github.com/liquidata-inc/ld/dolt/go/libraries/doltcore/schema"
)

// A result set schema understands how to map values from multiple schemas together into a final result schema.
type ResultSetSchema struct {
	mapping        map[schema.Schema]*rowconv.FieldMapping
	destSch        schema.Schema
	maxSchTag      uint64
	maxAssignedTag uint64
}

// Creates a new result set schema for the destination schema given. Successive calls to AddSchema will flesh out the
// mapping values for all source schemas.
func NewFromDestSchema(sch schema.Schema) (*ResultSetSchema, error) {
	maxTag, err := validateSchema(sch)
	if err != nil {
		return nil, err
	}
	return &ResultSetSchema{
		mapping:   make(map[schema.Schema]*rowconv.FieldMapping),
		destSch:   sch,
		maxSchTag: maxTag,
	}, nil
}

// Creates a new result set schema for the source schemas given and fills in schema values as necessary.
func NewFromSourceSchemas(sourceSchemas ...schema.Schema) (*ResultSetSchema, error) {
	var sch schema.Schema
	var rss *ResultSetSchema
	var err error

	if sch, err = ConcatSchemas(sourceSchemas...); err != nil {
		return nil, err
	}

	if rss, err = NewFromDestSchema(sch); err != nil {
		return &ResultSetSchema{}, err
	}

	for _, ss := range sourceSchemas {
		if err = rss.AddSchema(ss); err != nil {
			return nil, err
		}
	}

	return rss, nil
}

// Validates that the given schema is suitable for use as a result set. Result set schemas must use contiguous tags
// starting at 0.
func validateSchema(sch schema.Schema) (uint64, error) {
	valid := true
	expectedTag := uint64(0)
	sch.GetAllCols().IterInSortedOrder(func(tag uint64, col schema.Column) (stop bool) {
		if tag != expectedTag {
			valid = false
			return true
		}
		expectedTag++
		return false
	})

	if !valid {
		return 0, errors.New("Result set mappings must use contiguous tag numbers starting at 0")
	}

	return expectedTag - 1, nil
}

// Adds a schema to the result set mapping. The order of
func (rss *ResultSetSchema) AddSchema(sch schema.Schema) error {
	if rss.maxAssignedTag + uint64(len(sch.GetAllCols().GetColumns()) - 1) > rss.maxSchTag {
		return errors.New("No room for additional schema in mapping, result set schema too small")
	}

	fieldMapping := make(map[uint64]uint64)
	sch.GetAllCols().IterInSortedOrder(func(tag uint64, col schema.Column) (stop bool) {
		fieldMapping[tag] = rss.maxAssignedTag
		rss.maxAssignedTag++
		return false
	})

	mapping, err := rowconv.NewFieldMapping(sch, rss.destSch, fieldMapping)
	if err != nil {
		return err
	}

	rss.mapping[sch] = mapping
	return nil
}

// Concanates the given schemas together into a new one. This rewrites the tag numbers to be contiguous and
// starting from zero, and removes all keys and constraints. Types are preserved.
func ConcatSchemas(srcSchemas ...schema.Schema) (schema.Schema, error) {
	cols := make([]schema.Column, 0)
	var itag uint64
	for _, col := range srcSchemas {
		col.GetAllCols().Iter(func(tag uint64, col schema.Column) (stop bool) {
			cols = append(cols, schema.NewColumn(col.Name, itag, col.Kind, false))
			itag++
			return false
		})
	}
	colCollection, err := schema.NewColCollection(cols...)
	if err != nil {
		return nil, err
	}
	return schema.UnkeyedSchemaFromCols(colCollection), nil
}

// CombineRows writes all values from r2 into r1 and returns it. r1 must have the same schema as the result set.
func (rss *ResultSetSchema) CombineRows(r1 RowWithSchema, r2 RowWithSchema) RowWithSchema {
	if !schema.SchemasAreEqual(r1.Schema, rss.destSch) {
		panic("Cannot call CombineRows on a row with a different schema than the result set schema")
	}

	fieldMapping, ok := rss.mapping[r2.Schema]
	if !ok {
		panic (fmt.Sprintf("Unrecognized schema %v", r1.Schema))
	}

	r2.Row.IterCols(func(tag uint64, val types.Value) (stop bool) {
		var err error

		err = r1.SetColVal(fieldMapping.SrcToDest[tag], val)
		if err != nil {
			panic(err.Error())
		}
		return false
	})
	return r1
}