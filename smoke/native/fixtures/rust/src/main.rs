use axum::{routing::get, Router};

async fn answer() -> &'static str {
    "native-smoke"
}

#[tokio::main]
async fn main() {
    let app = Router::new().route("/", get(answer));
    let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
    println!("{}", listener.local_addr().unwrap().port());
    axum::serve(listener, app).await.unwrap();
}

#[tokio::test]
async fn answer_is_stable() {
    assert_eq!(answer().await, "native-smoke");
}
