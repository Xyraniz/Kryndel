#[inline(never)]
fn sum(values: &[i64]) -> i64 {
    values.iter().sum()
}

fn main() {
    let values = [1i64, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16];
    let mut index = 0;
    let mut total = 0i64;
    while index < 10_000 {
        total += sum(&values);
        index += 1;
    }
    println!("{total}");
}
