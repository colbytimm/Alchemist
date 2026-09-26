package query

import "strings"

// Function is one Cosmos DB system function, with its signature as the
// suggestion's detail.
type Function struct {
	Name      string
	Signature string
}

// functions follows the Azure Cosmos DB for NoSQL system function reference
// as published in 2025, by category: aggregate, array, conditional, date and
// time, item, mathematical, spatial, string, type checking, full-text, and
// vector. Names are written the way the reference writes them.
var functions = []Function{
	{"AVG", "AVG(expr)"},
	{"COUNT", "COUNT(expr)"},
	{"MAX", "MAX(expr)"},
	{"MIN", "MIN(expr)"},
	{"SUM", "SUM(expr)"},

	{"ARRAY_CONCAT", "ARRAY_CONCAT(array1, array2 [, arrayN])"},
	{"ARRAY_CONTAINS", "ARRAY_CONTAINS(array, expr [, partialMatch])"},
	{"ARRAY_CONTAINS_ALL", "ARRAY_CONTAINS_ALL(array, expr1 [, exprN])"},
	{"ARRAY_CONTAINS_ANY", "ARRAY_CONTAINS_ANY(array, expr1 [, exprN])"},
	{"ARRAY_LENGTH", "ARRAY_LENGTH(array)"},
	{"ARRAY_SLICE", "ARRAY_SLICE(array, start [, count])"},
	{"CHOOSE", "CHOOSE(index, expr1 [, exprN])"},
	{"ObjectToArray", "ObjectToArray(object [, keyName, valueName])"},
	{"SetIntersect", "SetIntersect(array1, array2)"},
	{"SetUnion", "SetUnion(array1, array2)"},

	{"IIF", "IIF(condition, trueValue, falseValue)"},

	{"DateTimeAdd", "DateTimeAdd(datePart, number, dateTime)"},
	{"DateTimeBin", "DateTimeBin(dateTime, datePart [, binSize, binStart])"},
	{"DateTimeDiff", "DateTimeDiff(datePart, startDateTime, endDateTime)"},
	{"DateTimeFromParts", "DateTimeFromParts(year, month, day [, hour, minute, second, fraction])"},
	{"DateTimePart", "DateTimePart(datePart, dateTime)"},
	{"DateTimeToTicks", "DateTimeToTicks(dateTime)"},
	{"DateTimeToTimestamp", "DateTimeToTimestamp(dateTime)"},
	{"GetCurrentDateTime", "GetCurrentDateTime()"},
	{"GetCurrentDateTimeStatic", "GetCurrentDateTimeStatic()"},
	{"GetCurrentTicks", "GetCurrentTicks()"},
	{"GetCurrentTicksStatic", "GetCurrentTicksStatic()"},
	{"GetCurrentTimestamp", "GetCurrentTimestamp()"},
	{"GetCurrentTimestampStatic", "GetCurrentTimestampStatic()"},
	{"TicksToDateTime", "TicksToDateTime(ticks)"},
	{"TimestampToDateTime", "TimestampToDateTime(timestamp)"},

	{"DocumentId", "DocumentId(item)"},

	{"ABS", "ABS(number)"},
	{"ACOS", "ACOS(number)"},
	{"ASIN", "ASIN(number)"},
	{"ATAN", "ATAN(number)"},
	{"ATN2", "ATN2(y, x)"},
	{"CEILING", "CEILING(number)"},
	{"COS", "COS(number)"},
	{"COT", "COT(number)"},
	{"DEGREES", "DEGREES(number)"},
	{"EXP", "EXP(number)"},
	{"FLOOR", "FLOOR(number)"},
	{"IntAdd", "IntAdd(int1, int2)"},
	{"IntBitAnd", "IntBitAnd(int1, int2)"},
	{"IntBitLeftShift", "IntBitLeftShift(int, shift)"},
	{"IntBitNot", "IntBitNot(int)"},
	{"IntBitOr", "IntBitOr(int1, int2)"},
	{"IntBitRightShift", "IntBitRightShift(int, shift)"},
	{"IntBitXor", "IntBitXor(int1, int2)"},
	{"IntDiv", "IntDiv(int1, int2)"},
	{"IntMod", "IntMod(int1, int2)"},
	{"IntMul", "IntMul(int1, int2)"},
	{"IntSub", "IntSub(int1, int2)"},
	{"LOG", "LOG(number [, base])"},
	{"LOG10", "LOG10(number)"},
	{"NumberBin", "NumberBin(number [, binSize])"},
	{"PI", "PI()"},
	{"POWER", "POWER(number, exponent)"},
	{"RADIANS", "RADIANS(number)"},
	{"RAND", "RAND()"},
	{"ROUND", "ROUND(number)"},
	{"SIGN", "SIGN(number)"},
	{"SIN", "SIN(number)"},
	{"SQRT", "SQRT(number)"},
	{"SQUARE", "SQUARE(number)"},
	{"TAN", "TAN(number)"},
	{"TRUNC", "TRUNC(number)"},

	{"ST_AREA", "ST_AREA(spatial)"},
	{"ST_DISTANCE", "ST_DISTANCE(spatial1, spatial2)"},
	{"ST_INTERSECTS", "ST_INTERSECTS(spatial1, spatial2)"},
	{"ST_ISVALID", "ST_ISVALID(spatial)"},
	{"ST_ISVALIDDETAILED", "ST_ISVALIDDETAILED(spatial)"},
	{"ST_WITHIN", "ST_WITHIN(spatial1, spatial2)"},

	{"CONCAT", "CONCAT(string1, string2 [, stringN])"},
	{"CONTAINS", "CONTAINS(string, search [, ignoreCase])"},
	{"ENDSWITH", "ENDSWITH(string, suffix [, ignoreCase])"},
	{"INDEX_OF", "INDEX_OF(string, search [, start])"},
	{"LEFT", "LEFT(string, count)"},
	{"LENGTH", "LENGTH(string)"},
	{"LOWER", "LOWER(string)"},
	{"LTRIM", "LTRIM(string [, chars])"},
	{"REGEXMATCH", "REGEXMATCH(string, pattern [, flags])"},
	{"REPLACE", "REPLACE(string, search, replacement)"},
	{"REPLICATE", "REPLICATE(string, count)"},
	{"REVERSE", "REVERSE(string)"},
	{"RIGHT", "RIGHT(string, count)"},
	{"RTRIM", "RTRIM(string [, chars])"},
	{"STARTSWITH", "STARTSWITH(string, prefix [, ignoreCase])"},
	{"STRINGEQUALS", "STRINGEQUALS(string1, string2 [, ignoreCase])"},
	{"StringJoin", "StringJoin(array, separator)"},
	{"StringSplit", "StringSplit(string, separator)"},
	{"StringToArray", "StringToArray(string)"},
	{"StringToBoolean", "StringToBoolean(string)"},
	{"StringToNull", "StringToNull(string)"},
	{"StringToNumber", "StringToNumber(string)"},
	{"StringToObject", "StringToObject(string)"},
	{"SUBSTRING", "SUBSTRING(string, start, length)"},
	{"ToString", "ToString(expr)"},
	{"TRIM", "TRIM(string [, chars])"},
	{"UPPER", "UPPER(string)"},

	{"IS_ARRAY", "IS_ARRAY(expr)"},
	{"IS_BOOL", "IS_BOOL(expr)"},
	{"IS_DEFINED", "IS_DEFINED(expr)"},
	{"IS_FINITE_NUMBER", "IS_FINITE_NUMBER(expr)"},
	{"IS_INTEGER", "IS_INTEGER(expr)"},
	{"IS_NULL", "IS_NULL(expr)"},
	{"IS_NUMBER", "IS_NUMBER(expr)"},
	{"IS_OBJECT", "IS_OBJECT(expr)"},
	{"IS_PRIMITIVE", "IS_PRIMITIVE(expr)"},
	{"IS_STRING", "IS_STRING(expr)"},

	{"FullTextContains", "FullTextContains(path, search)"},
	{"FullTextContainsAll", "FullTextContainsAll(path, search1 [, searchN])"},
	{"FullTextContainsAny", "FullTextContainsAny(path, search1 [, searchN])"},
	{"FullTextScore", "FullTextScore(path, search1 [, searchN])"},
	{"RRF", "RRF(score1, score2 [, scoreN])"},
	{"VectorDistance", "VectorDistance(vector1, vector2 [, exact, options])"},
}

func Functions() []Function {
	return append([]Function(nil), functions...)
}

// builtinFunctions spells each function the way the reference does, by its
// upper-cased name: the service matches function names in any case.
var builtinFunctions = functionsByName()

func functionsByName() map[string]string {
	names := make(map[string]string, len(functions))
	for _, f := range functions {
		names[strings.ToUpper(f.Name)] = f.Name
	}
	return names
}
