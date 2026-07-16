import { useEffect, useRef } from "react";

type MasterCheckboxProps = {
  selectableIds: string[];
  selectedIds: Set<string>;
  onChange: (next: Set<string>) => void;
  label?: string;
};

// Header checkbox: selects every selectable row, renders indeterminate when
// only some are checked.
export function MasterCheckbox({
  selectableIds,
  selectedIds,
  onChange,
  label = "Select all rows",
}: MasterCheckboxProps) {
  const allChecked = selectableIds.length > 0 && selectableIds.every((id) => selectedIds.has(id));
  const someChecked = selectableIds.some((id) => selectedIds.has(id));
  const ref = useRef<HTMLInputElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.indeterminate = !allChecked && someChecked;
  }, [allChecked, someChecked]);
  return (
    <input
      ref={ref}
      type="checkbox"
      checked={allChecked}
      aria-label={label}
      onChange={(e) => onChange(e.target.checked ? new Set(selectableIds) : new Set())}
      className="size-4 cursor-pointer rounded border-input accent-primary"
    />
  );
}

type RowCheckboxProps = {
  id: string;
  checked: boolean;
  disabled?: boolean;
  label: string;
  onChange: (id: string, checked: boolean) => void;
};

export function RowCheckbox({ id, checked, disabled, label, onChange }: RowCheckboxProps) {
  return (
    <input
      type="checkbox"
      checked={checked}
      disabled={disabled}
      aria-label={label}
      onChange={(e) => onChange(id, e.target.checked)}
      className="size-4 cursor-pointer rounded border-input accent-primary disabled:cursor-not-allowed disabled:opacity-50"
    />
  );
}
