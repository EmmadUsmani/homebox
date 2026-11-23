import { expect, it } from "vitest";
import { statePill } from "./pill";

it.each([
    ["running", "Running", "good"],
    ["stopped", "Stopped", "idle"],
    ["broken", "Broken", "bad"],
    ["provisioning", "Provisioning", "warn"],
    ["provision-failed", "Provisioning failed", "bad"],
    ["toString", "toString", "idle"],
    ["", "Unknown", "idle"],
])("maps %s to a labelled pill", (state, label, tone) => {
    expect(statePill(state)).toEqual({ label, tone });
});
