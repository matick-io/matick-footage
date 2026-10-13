import { useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Button, Dialog, Input, Select } from "@khaos-systems/ui-core";
import { ListVideo } from "lucide-react";
import { api } from "../api";
import { setActiveSelect, useActiveSelect } from "./useActiveSelect";

const NEW = "__new__";

/** Chooses which select "Add to select" fills, or starts a new one. */
export function SelectPicker() {
  const { active, selects } = useActiveSelect();
  const [naming, setNaming] = useState(false);
  const [name, setName] = useState("");
  const qc = useQueryClient();

  const create = async () => {
    const sel = await api.createSelect(name.trim() || "Untitled select");
    await qc.invalidateQueries({ queryKey: ["selects"] });
    setActiveSelect(sel.id);
    setNaming(false);
    setName("");
  };

  return (
    <>
      <Select
        size="small"
        value={active ? String(active.id) : ""}
        placeholder={
          <span className="flex items-center gap-1.5">
            <ListVideo className="h-3.5 w-3.5" /> No select
          </span>
        }
        onValueChange={(v) => (v === NEW ? setNaming(true) : setActiveSelect(Number(v)))}
        options={[
          ...selects.map((s) => ({ value: String(s.id), label: `${s.name} (${s.item_count})` })),
          { value: NEW, label: "New select…" },
        ]}
      />
      <Dialog open={naming} onOpenChange={setNaming} title="New select" size="small">
        <form
          className="flex flex-col gap-3 p-4"
          onSubmit={(e) => {
            e.preventDefault();
            void create();
          }}
        >
          <Input autoFocus placeholder="Name, e.g. Warden short" value={name} onChange={(e) => setName(e.target.value)} />
          <div className="flex justify-end gap-2">
            <Button variant="ghost" type="button" onClick={() => setNaming(false)}>
              Cancel
            </Button>
            <Button variant="accent" type="submit">
              Create
            </Button>
          </div>
        </form>
      </Dialog>
    </>
  );
}
