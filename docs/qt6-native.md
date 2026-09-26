# Native Qt 6 bindings for Kryndel

## Decision

The Qt integration is native: Kryndel programs must not need CPython or PyQt6 at run time. The implementation will bind to Qt 6's C++ libraries and expose a Kryndel API with coverage matching the public Qt surface wrapped by a pinned PyQt6 release. Python-only conveniences are represented with Kryndel types and call semantics; this is not a Python compatibility runtime.

Qt remains an optional application dependency. The Kryndel compiler and its non-GUI applications stay independent of Qt.

## Scope of complete support

Calling the integration complete requires an inventory generated from the selected PyQt6 release and matching coverage for its public modules, classes, constructors, methods, overloads, enums, flags, properties, signals, slots, and value conversions. The binding must also preserve QObject ownership, destruction, thread affinity, queued delivery, and the Qt event loop.

A small set of Qt Widgets is only an early milestone. It does not satisfy full PyQt6 coverage. Coverage gaps must remain visible in the generated inventory instead of being described as supported.

## Runtime architecture

Kryndel's release runtime is pure Go and is built with CGO disabled. Keep that compiler/runtime artifact independent of Qt and avoid linking the compiler to Qt. A separate native adapter can link selected Qt modules and expose stable C-ABI entry points to the Kryndel interpreter.

Kryndel's current FFI supports integer and opaque-pointer C signatures. C++ classes, overloaded methods, Qt value types, and callbacks cannot be called safely by exposing Qt's C++ symbols directly. The adapter and generated binding layer must provide typed wrappers, stable handles, checked argument conversion, deterministic cleanup, and event delivery on the GUI thread.

QObject reflection can help with runtime properties, invokable methods, and signals, but it does not describe every public C++ method or non-QObject type. Reflection alone is not a complete binding generator. Full coverage needs generated wrappers from authoritative Qt API metadata plus explicit handling for types that cannot be represented generically.

## Packaging

Building the optional adapter requires a Qt 6 development installation, a matching C++ toolchain, and CMake. Running an application requires the Qt runtime libraries and platform plugins for the modules it uses. Package only the libraries and plugins needed by that application; loading the Qt Widgets module must not load every Qt module.

Qt modules have different license availability. Audit the selected modules before redistribution, and retain Qt's deployment metadata, notices, and relinking requirements for the chosen license. The Kryndel CLI release must continue to work without Qt installed.

## Completion gates

- Pin and record the upstream PyQt6/Qt 6 API version and generate a reproducible module/type inventory.
- Generate and compile the native binding for every public module in that inventory.
- Preserve event-loop behavior, signal/slot connections, object ownership, overload selection, enum/flag conversions, and error reporting across the Kryndel boundary.
- Build the optional adapter for Kryndel's supported desktop targets and verify runtime deployment contains only required Qt modules and plugins.
- Publish the Kryndel package/API reference and a native GUI application example with the complete coverage inventory.

## References

- [PyQt overview and platform/licensing model](https://www.riverbankcomputing.com/software/pyqt)
- [Qt's meta-object system](https://doc.qt.io/qt-6/metaobjects.html)
- [Qt application deployment](https://doc.qt.io/qt-6/deployment.html)
- [Qt licensing](https://doc.qt.io/qt-6/licensing.html)
