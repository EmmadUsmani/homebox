const std = @import("std");
extern "c" fn puts([*:0]const u8) c_int;

pub fn main() void {
    _ = puts("native-smoke");
}

test "integer arithmetic" {
    try std.testing.expectEqual(@as(u32, 42), 21 * 2);
}
