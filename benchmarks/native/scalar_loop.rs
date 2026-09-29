#[inline(never)]
fn transform(value: i64) -> i64 {
    value * 3 + 1
}

fn main() {
    let mut index = 0i64;
    let mut total = 0i64;
    while index < 200_000 {
        total += transform(index);
        index += 1;
    }
    println!("{total}");
}
