package policy

func (e *Engine) AddBinding(b Binding) error {
	if err := validateBinding(b); err != nil {
		return err
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	snap := cloneSnapshot(e.current.Load())
	key := bindingKey(b.Src, b.Dst, b.Scenario)
	if _, ok := snap.byKey[key]; ok {
		return ErrDuplicateBinding
	}
	nb := cloneBindingPtr(&b)
	snap.all = append(snap.all, nb)
	snap.byKey[key] = nb
	snap.out[nb.Src] = append(snap.out[nb.Src], nb)
	e.current.Store(snap)
	return nil
}

func (e *Engine) UpdateBinding(b Binding) error {
	if err := validateBinding(b); err != nil {
		return err
	}
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	cur := e.current.Load()
	key := bindingKey(b.Src, b.Dst, b.Scenario)
	if cur == nil || cur.byKey[key] == nil {
		return ErrBindingNotFound
	}
	snap := cloneSnapshot(cur)
	existing := snap.byKey[key]
	existing.Conditions = cloneConditions(b.Conditions)
	existing.Enabled = b.Enabled
	e.current.Store(snap)
	return nil
}

func (e *Engine) RemoveBinding(src, dst, scenario string) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	cur := e.current.Load()
	key := bindingKey(src, dst, scenario)
	if cur == nil || cur.byKey[key] == nil {
		return ErrBindingNotFound
	}
	kept := make([]Binding, 0, len(cur.all)-1)
	for _, b := range cur.all {
		if bindingKey(b.Src, b.Dst, b.Scenario) == key {
			continue
		}
		kept = append(kept, copyBindingValue(b))
	}
	snap, err := snapshotFromBindings(kept)
	if err != nil {
		return err
	}
	e.current.Store(snap)
	return nil
}

func (e *Engine) SetEnabled(src, dst, scenario string, enabled bool) error {
	e.writeMu.Lock()
	defer e.writeMu.Unlock()
	cur := e.current.Load()
	key := bindingKey(src, dst, scenario)
	if cur == nil || cur.byKey[key] == nil {
		return ErrBindingNotFound
	}
	snap := cloneSnapshot(cur)
	snap.byKey[key].Enabled = enabled
	e.current.Store(snap)
	return nil
}

func (e *Engine) GetBinding(src, dst, scenario string) (Binding, bool) {
	snap := e.current.Load()
	if snap == nil {
		return Binding{}, false
	}
	b := snap.byKey[bindingKey(src, dst, scenario)]
	if b == nil {
		return Binding{}, false
	}
	return copyBindingValue(b), true
}

func (e *Engine) ListBindings() []Binding {
	snap := e.current.Load()
	if snap == nil || len(snap.all) == 0 {
		return []Binding{}
	}
	out := make([]Binding, len(snap.all))
	for i, b := range snap.all {
		out[i] = copyBindingValue(b)
	}
	return out
}
