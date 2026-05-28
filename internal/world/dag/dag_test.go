package dag

import "testing"

func TestInternDedup(t *testing.T) {
	d := NewDedup()
	var counter uint32
	next := func() uint32 { counter++; return counter - 1 }
	a := d.Intern(Hash([]byte("hello")), next)
	b := d.Intern(Hash([]byte("hello")), next)
	c := d.Intern(Hash([]byte("world")), next)
	if a != b {
		t.Fatalf("identical payloads should intern to same id: %d %d", a, b)
	}
	if c == a {
		t.Fatalf("different payloads should differ")
	}
	if d.Len() != 2 {
		t.Fatalf("len = %d want 2", d.Len())
	}
}

func TestHashWordsDeterministic(t *testing.T) {
	a := HashWords([]uint32{1, 2, 3})
	b := HashWords([]uint32{1, 2, 3})
	if a != b {
		t.Fatalf("HashWords not deterministic")
	}
}
