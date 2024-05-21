// Copyright (c) 2016 OpenM++
// This code is licensed under the MIT license (see LICENSE.txt for details)

package db

import (
	"errors"
	"fmt"
	"strconv"
)

// CellAcc is value of output table accumulator.
type CellAcc struct {
	cellIdValue     // dimensions as enum id's and value
	AccId       int // output table accumulator id
	SubId       int // output table subvalue id
}

// CellCodeAcc is value of output table accumulator.
// Dimension(s) items are enum codes, not enum ids.
type CellCodeAcc struct {
	cellCodeValue     // dimensions as enum codes and value
	AccId         int // output table accumulator id
	SubId         int // output table subvalue id
}

// CellAccConverter is a converter for output table accumulator to implement CsvConverter interface.
type CellAccConverter struct {
	CellTableConverter // model metadata and output table name
}

// return true if csv converter is using enum id's for dimensions
func (cellCvt *CellAccConverter) IsUseEnumId() bool { return cellCvt.IsIdCsv }

// CsvFileName return file name of csv file to store output table accumulator rows
func (cellCvt *CellAccConverter) CsvFileName() (string, error) {

	// find output table by name
	_, err := cellCvt.tableByName()
	if err != nil {
		return "", err
	}

	// make csv file name
	if cellCvt.IsIdCsv {
		return cellCvt.Name + ".id.acc.csv", nil
	}
	return cellCvt.Name + ".acc.csv", nil
}

// CsvHeader return first line for csv file: column names.
// Column names can be like: acc_name,sub_id,dim0,dim1,acc_value
// or if IsIdCsv is true then: acc_id,sub_id,dim0,dim1,acc_value
func (cellCvt *CellAccConverter) CsvHeader() ([]string, error) {

	// find output table by name
	table, err := cellCvt.tableByName()
	if err != nil {
		return []string{}, err
	}

	// make first line columns
	h := make([]string, table.Rank+3)

	if cellCvt.IsIdCsv {
		h[0] = "acc_id"
	} else {
		h[0] = "acc_name"
	}

	h[1] = "sub_id"
	for k := range table.Dim {
		h[k+2] = table.Dim[k].Name
	}
	h[table.Rank+2] = "acc_value"

	return h, nil
}

// KeyIds return converter to copy primary key: (acc_id, sub_id, dimension ids) into key []int.
//
// Converter will return error if len(key) not equal to row key size.
func (cellCvt *CellAccConverter) KeyIds(name string) (func(interface{}, []int) error, error) {

	cvt := func(src interface{}, key []int) error {

		cell, ok := src.(CellAcc)
		if !ok {
			return errors.New("invalid type, expected: CellAcc (internal error): " + name)
		}

		n := len(cell.DimIds)
		if len(key) != n+2 {
			return errors.New("invalid size of key buffer, expected: " + strconv.Itoa(n+2) + ": " + name)
		}

		key[0] = cell.AccId
		key[1] = cell.SubId

		for k, e := range cell.DimIds {
			key[k+2] = e
		}
		return nil
	}

	return cvt, nil
}

// ToCsvIdRow return converter from output table cell (acc_id, sub_id, dimensions, value) to csv id's row []string.
//
// Converter return isNotEmpty flag, it return false if IsNoZero or IsNoNull is set and cell value is empty or zero.
// Converter simply does Sprint() for each dimension item id, accumulator id, subvalue number and value.
// Converter will return error if len(row) not equal to number of fields in csv record.
func (cellCvt *CellAccConverter) ToCsvIdRow() (func(interface{}, []string) (bool, error), error) {

	// find output table by name
	_, err := cellCvt.tableByName()
	if err != nil {
		return nil, err
	}

	// return converter from id based cell to csv string array
	cvt := func(src interface{}, row []string) (bool, error) {

		cell, ok := src.(CellAcc)
		if !ok {
			return false, errors.New("invalid type, expected: CellAcc (internal error): " + cellCvt.Name)
		}

		n := len(cell.DimIds)
		if len(row) != n+3 {
			return false, errors.New("invalid size of csv row buffer, expected: " + strconv.Itoa(n+3) + ": " + cellCvt.Name)
		}

		row[0] = fmt.Sprint(cell.AccId)
		row[1] = fmt.Sprint(cell.SubId)

		for k, e := range cell.DimIds {
			row[k+2] = fmt.Sprint(e)
		}

		// use "null" string for db NULL values and format for model float types
		isNotEmpty := true

		if cell.IsNull {
			row[n+2] = "null"
			isNotEmpty = !cellCvt.IsNoNullCsv
		} else {

			if cellCvt.IsNoZeroCsv {
				fv, ok := cell.Value.(float64)
				isNotEmpty = ok && fv != 0
			}

			if cellCvt.DoubleFmt != "" {
				row[n+2] = fmt.Sprintf(cellCvt.DoubleFmt, cell.Value)
			} else {
				row[n+2] = fmt.Sprint(cell.Value)
			}
		}
		return isNotEmpty, nil
	}

	return cvt, nil
}

// ToCsvRow return converter from output table cell (acc_id, sub_id, dimensions, value)
// to csv row []string (acc_name, sub_id, dimensions, value).
//
// Converter return isNotEmpty flag, it return false if IsNoZero or IsNoNull is set and cell value is empty or zero.
// Converter will return error if len(row) not equal to number of fields in csv record.
// If dimension type is enum based then csv row is enum code and cell.DimIds is enum id.
func (cellCvt *CellAccConverter) ToCsvRow() (func(interface{}, []string) (bool, error), error) {

	// find output table by name
	table, err := cellCvt.tableByName()
	if err != nil {
		return nil, err
	}

	// for each dimension create converter from item id to code
	fd := make([]func(itemId int) (string, error), table.Rank)

	for k := 0; k < table.Rank; k++ {
		f, err := table.Dim[k].typeOf.itemIdToCode(cellCvt.Name+"."+table.Dim[k].Name, table.Dim[k].IsTotal)
		if err != nil {
			return nil, err
		}
		fd[k] = f
	}

	cvt := func(src interface{}, row []string) (bool, error) {

		cell, ok := src.(CellAcc)
		if !ok {
			return false, errors.New("invalid type, expected: output table accumulator cell (internal error): " + cellCvt.Name)
		}

		n := len(cell.DimIds)
		if len(row) != n+3 {
			return false, errors.New("invalid size of csv row buffer, expected: " + strconv.Itoa(n+3) + ": " + cellCvt.Name)
		}

		row[0] = table.Acc[cell.AccId].Name
		row[1] = fmt.Sprint(cell.SubId)

		// convert dimension item id to code
		for k, e := range cell.DimIds {
			v, err := fd[k](e)
			if err != nil {
				return false, err
			}
			row[k+2] = v
		}

		// use "null" string for db NULL values and format for model float types
		isNotEmpty := true

		if cell.IsNull {
			row[n+2] = "null"
			isNotEmpty = !cellCvt.IsNoNullCsv
		} else {

			if cellCvt.IsNoZeroCsv {
				fv, ok := cell.Value.(float64)
				isNotEmpty = ok && fv != 0
			}

			if cellCvt.DoubleFmt != "" {
				row[n+2] = fmt.Sprintf(cellCvt.DoubleFmt, cell.Value)
			} else {
				row[n+2] = fmt.Sprint(cell.Value)
			}
		}
		return isNotEmpty, nil
	}

	return cvt, nil
}

// CsvToCell return closure to convert csv row []string to output table accumulator cell (dimensions and value).
//
// It does return error if len(row) not equal to number of fields in cell db-record.
// If dimension type is enum based then csv row is enum code and it is converted into cell.DimIds (into dimension type type enum ids).
func (cellCvt *CellAccConverter) ToCell() (func(row []string) (interface{}, error), error) {

	// find output table by name
	table, err := cellCvt.tableByName()
	if err != nil {
		return nil, err
	}

	// for each dimension create converter from item code to id
	fd := make([]func(src string) (int, error), table.Rank)

	for k := 0; k < table.Rank; k++ {
		f, err := table.Dim[k].typeOf.itemCodeToId(cellCvt.Name+"."+table.Dim[k].Name, table.Dim[k].IsTotal)
		if err != nil {
			return nil, err
		}
		fd[k] = f
	}

	// do conversion
	cvt := func(row []string) (interface{}, error) {

		// make conversion buffer and check input csv row size
		cell := CellAcc{cellIdValue: cellIdValue{DimIds: make([]int, table.Rank)}}

		n := len(cell.DimIds)
		if len(row) != n+3 {
			return nil, errors.New("invalid size of csv row, expected: " + strconv.Itoa(n+3) + ": " + cellCvt.Name)
		}

		// accumulator id by name
		cell.AccId = -1
		for k := range table.Acc {
			if row[0] == table.Acc[k].Name {
				cell.AccId = k
				break
			}
		}
		if cell.AccId < 0 {
			return nil, errors.New("invalid accumulator name: " + row[0] + " output table: " + cellCvt.Name)
		}

		// subvalue number
		nSub, err := strconv.Atoi(row[1])
		if err != nil {
			return nil, err
		}
		/* validation done at writing
		if subCount == 1 && nSub != defaultSubId || nSub < 0 || nSub >= subCount {
			return nil, errors.New("invalid sub-value id: " + strconv.Itoa(nSub) + " output table: " + name)
		}
		*/
		cell.SubId = nSub

		// convert dimensions: enum code to enum id or integer value for simple type dimension
		for k := range cell.DimIds {
			i, err := fd[k](row[k+2])
			if err != nil {
				return nil, err
			}
			cell.DimIds[k] = i
		}

		// value conversion
		cell.IsNull = row[n+2] == "" || row[n+2] == "null"

		if cell.IsNull {
			cell.Value = 0.0
		} else {
			v, err := strconv.ParseFloat(row[n+2], 64)
			if err != nil {
				return nil, err
			}
			cell.Value = v
		}
		return cell, nil
	}

	return cvt, nil
}

// IdToCodeCell return converter from output table cell of ids: (acc_id, sub_id, dimensions, value)
// to cell of codes: (acc_id, sub_id, dimensions as enum code, value)
//
// If dimension type is enum based then dimensions enum ids can be converted to enum code.
// If dimension type is simple (bool or int) then dimension value converted to string.
func (cellCvt *CellAccConverter) IdToCodeCell(modelDef *ModelMeta, name string) (func(interface{}) (interface{}, error), error) {

	// find output table by name
	table, err := cellCvt.tableByName()
	if err != nil {
		return nil, err
	}

	// for each dimension create converter from item id to code
	fd := make([]func(itemId int) (string, error), table.Rank)

	for k := 0; k < table.Rank; k++ {
		f, err := table.Dim[k].typeOf.itemIdToCode(name+"."+table.Dim[k].Name, table.Dim[k].IsTotal)
		if err != nil {
			return nil, err
		}
		fd[k] = f
	}

	// create cell converter
	cvt := func(src interface{}) (interface{}, error) {

		srcCell, ok := src.(CellAcc)
		if !ok {
			return nil, errors.New("invalid type, expected: output table accumulator id cell (internal error): " + name)
		}
		if len(srcCell.DimIds) != table.Rank {
			return nil, errors.New("invalid cell rank: " + strconv.Itoa(len(srcCell.DimIds)) + ", expected: " + strconv.Itoa(table.Rank) + ": " + name)
		}

		dstCell := CellCodeAcc{
			cellCodeValue: cellCodeValue{
				Dims:   make([]string, table.Rank),
				IsNull: srcCell.IsNull,
				Value:  srcCell.Value,
			},
			AccId: srcCell.AccId,
			SubId: srcCell.SubId,
		}

		// convert dimension item id to code
		for k := range srcCell.DimIds {
			v, err := fd[k](srcCell.DimIds[k])
			if err != nil {
				return nil, err
			}
			dstCell.Dims[k] = v
		}

		return dstCell, nil // converted OK
	}

	return cvt, nil
}
