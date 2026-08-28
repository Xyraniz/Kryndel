# Discord en Kryndel

El paquete `packages/discord` proporciona una integración pequeña y explícita para bots. `bot(token, intents)` valida el token localmente y construye un `Bot`; `Bot.run()` abre un WebSocket seguro hacia Discord Gateway v10, envía un payload Identify con el token y la máscara de intents, recibe el primer frame del Gateway y cierra la conexión de forma cooperativa. `Bot.on_message(command)` es el punto tipado para registrar comandos en la superficie inicial; la API no inventa eventos ni simula respuestas.

| Elemento | Tipo | Semántica |
| --- | --- | --- |
| `Bot.token` | `String` | Se mantiene en memoria del programa y nunca se imprime. |
| `Bot.intents` | `Array[String]` | `Guilds`, `GuildMembers`, `GuildMessages` y `MessageContent` se convierten a bits explícitos. |
| `Bot.gateway` | `String` | URL WebSocket `wss://gateway.discord.gg/?v=10&encoding=json`. |
| `bot(token, intents)` | `Result[Bot,String]` | Rechaza un token vacío. |
| `Bot.run()` | `Result[Nil,String]` | Ejecuta handshake, Identify, lectura bounded y cierre. |
| `Bot.on_message(command)` | `Result[Nil,String]` | Rechaza registros vacíos y mantiene un contrato determinista. |

La capa WebSocket implementa el handshake RFC 6455, valida `Sec-WebSocket-Accept`, usa TLS 1.2 o superior para `wss`, enmascara frames de cliente, responde a ping con pong, rechaza frames mayores que el límite de entrada y trata el cierre remoto como error explícito. `WebSocket` es un tipo no-Copy, por lo que no puede cruzar canales ni duplicarse implícitamente.

Las llamadas HTTP autenticadas se realizan con `http_request_auth`. El token solo se coloca en el encabezado `Authorization: Bearer ...`; los diagnósticos de estado y transporte no incluyen el valor secreto. En producción, el token debe llegar por una variable de entorno leída con `std/env.kry`, no escribirse en el código fuente ni almacenarse en el repositorio.

> La integración inicial cubre la conexión real al Gateway y la frontera API segura. No pretende ocultar la complejidad de reconexión, rate limits, sharding, persistencia de secuencias o callbacks asincrónicos; esas capacidades deben añadirse como contratos explícitos y pruebas contra un servidor de prueba controlado.
