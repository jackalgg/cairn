package reindent

import "testing"

// TestSchemaReferentialIntegrity: every child/elem type a field opens must
// itself exist in schemaTable (or be the stringMap wildcard / scalar ""). A
// typo in the table would silently degrade a whole subtree to untyped
// placement — the exact bug class the typed entries exist to prevent.
func TestSchemaReferentialIntegrity(t *testing.T) {
	known := func(name string) bool {
		if name == "" || name == stringMap {
			return true
		}
		_, ok := schemaTable[name]
		return ok
	}
	for typ, fields := range schemaTable {
		for key, f := range fields {
			if !known(f.child) {
				t.Errorf("%s.%s opens unknown mapping type %q", typ, key, f.child)
			}
			if !known(f.elem) {
				t.Errorf("%s.%s opens sequence of unknown type %q", typ, key, f.elem)
			}
		}
	}
	for kind, typ := range kindType {
		if !known(typ) {
			t.Errorf("kindType[%q] points at unknown type %q", kind, typ)
		}
	}
}

// TestSchemaNoAdjacentFieldNames: no two field names within one type may sit
// within one edit of each other. Both being declared makes them safe from the
// typo pass itself, but a USER typo of either would then always be ambiguous —
// and worse, a table like that usually means one entry is itself a typo.
// (Documented exception: clusterIP/clusterIPs, both real ServiceSpec fields.)
func TestSchemaNoAdjacentFieldNames(t *testing.T) {
	allowed := map[string]bool{
		"ServiceSpec.clusterIP/clusterIPs": true,
	}
	for typ, fields := range schemaTable {
		names := make([]string, 0, len(fields))
		for k := range fields {
			names = append(names, k)
		}
		for i := 0; i < len(names); i++ {
			for j := i + 1; j < len(names); j++ {
				a, b := names[i], names[j]
				if a > b {
					a, b = b, a
				}
				if editDistance(a, b) <= 1 && !allowed[typ+"."+a+"/"+b] {
					t.Errorf("%s: fields %q and %q are within one edit of each other", typ, a, b)
				}
			}
		}
	}
}
