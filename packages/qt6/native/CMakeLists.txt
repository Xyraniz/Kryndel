cmake_minimum_required(VERSION 3.21)
project(kryndel_qt6_bridge LANGUAGES CXX)

include(GNUInstallDirs)

set(KRY_QT6_COMPONENTS
    3DAnimation
    3DCore
    3DExtras
    3DInput
    3DLogic
    3DRender
    Bluetooth
    Charts
    Core
    DataVisualization
    DBus
    Designer
    Graphs
    GraphsWidgets
    Gui
    Help
    Multimedia
    MultimediaWidgets
    Network
    NetworkAuth
    Nfc
    OpenGL
    OpenGLWidgets
    Pdf
    PdfWidgets
    Positioning
    PrintSupport
    Qml
    Quick
    Quick3D
    QuickWidgets
    RemoteObjects
    Sensors
    SerialPort
    SpatialAudio
    Sql
    StateMachine
    Svg
    SvgWidgets
    Test
    TextToSpeech
    WebChannel
    WebSockets
    Widgets
    Xml
)
find_package(Qt6 6.11 REQUIRED COMPONENTS ${KRY_QT6_COMPONENTS})
if(WIN32)
    find_package(Qt6 6.11 QUIET COMPONENTS AxContainer)
    if(TARGET Qt6::AxContainer)
        list(APPEND KRY_QT6_COMPONENTS AxContainer)
    endif()
endif()

# WebEngine is shipped as a separate Qt add-on. Link every component that the
# selected kit provides so the generated module factories can expose it.
foreach(component IN ITEMS WebEngineCore WebEngineQuick WebEngineWidgets)
    find_package(Qt6 6.11 QUIET COMPONENTS ${component})
    if(TARGET Qt6::${component})
        list(APPEND KRY_QT6_COMPONENTS ${component})
    endif()
endforeach()

file(GLOB KRY_QT6_FACTORY_SOURCES CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/generated_modules/qt6_factory_*.cpp"
)
file(GLOB KRY_QT6_METHOD_SOURCES CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/generated_methods/qt6_methods_*.cpp"
)
file(GLOB KRY_QT6_CONSTRUCTOR_SOURCES CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/generated_constructors/qt6_constructors_*.cpp"
)
file(GLOB KRY_QT6_VALUE_METHOD_SOURCES CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/generated_value_methods/qt6_value_methods_*.cpp"
)
file(GLOB KRY_QT6_VALUE_CONSTRUCTOR_SOURCES CONFIGURE_DEPENDS
    "${CMAKE_CURRENT_SOURCE_DIR}/generated_value_constructors/qt6_value_constructors_*.cpp"
)
add_library(kry_qt6 SHARED qt6_bridge.cpp ${KRY_QT6_FACTORY_SOURCES} ${KRY_QT6_METHOD_SOURCES} ${KRY_QT6_CONSTRUCTOR_SOURCES}
    ${KRY_QT6_VALUE_METHOD_SOURCES} ${KRY_QT6_VALUE_CONSTRUCTOR_SOURCES})
target_compile_features(kry_qt6 PRIVATE cxx_std_17)
set(KRY_QT6_TARGETS)
foreach(component IN LISTS KRY_QT6_COMPONENTS)
    list(APPEND KRY_QT6_TARGETS Qt6::${component})
endforeach()
target_link_libraries(kry_qt6 PRIVATE ${KRY_QT6_TARGETS})
set_target_properties(kry_qt6 PROPERTIES CXX_VISIBILITY_PRESET hidden VISIBILITY_INLINES_HIDDEN YES)

if(WIN32)
    set_target_properties(kry_qt6 PROPERTIES PREFIX "" SUFFIX ".dll")
elseif(APPLE)
    set_target_properties(kry_qt6 PROPERTIES PREFIX "lib" SUFFIX ".dylib")
else()
    set_target_properties(kry_qt6 PROPERTIES PREFIX "lib" SUFFIX ".so")
endif()

install(TARGETS kry_qt6
    RUNTIME DESTINATION ${CMAKE_INSTALL_BINDIR}
    LIBRARY DESTINATION ${CMAKE_INSTALL_LIBDIR}
    ARCHIVE DESTINATION ${CMAKE_INSTALL_LIBDIR})
install(FILES qt6_bridge.h DESTINATION ${CMAKE_INSTALL_INCLUDEDIR}/kryndel/qt6)
