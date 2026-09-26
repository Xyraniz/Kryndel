#include "qt6_bridge.h"

#include <QtGui/QAction>
#include <QtWidgets/QApplication>
#include <QtWidgets/QButtonGroup>
#include <QtCore/QByteArray>
#include <QtWidgets/QCalendarWidget>
#include <QtWidgets/QCheckBox>
#include <QtGui/QColor>
#include <QtWidgets/QColorDialog>
#include <QtWidgets/QCommandLinkButton>
#include <QtWidgets/QComboBox>
#include <QtCore/QCoreApplication>
#include <QtCore/QDate>
#include <QtWidgets/QDateEdit>
#include <QtCore/QDateTime>
#include <QtWidgets/QDateTimeEdit>
#include <QtWidgets/QDialog>
#include <QtWidgets/QDialogButtonBox>
#include <QtWidgets/QDial>
#include <QtWidgets/QDockWidget>
#include <QtWidgets/QDoubleSpinBox>
#include <QtCore/QEventLoop>
#include <QtCore/QFile>
#include <QtWidgets/QFileDialog>
#include <QtCore/QFileSystemWatcher>
#include <QtGui/QFont>
#include <QtWidgets/QFontComboBox>
#include <QtWidgets/QFontDialog>
#include <QtWidgets/QFormLayout>
#include <QtWidgets/QFrame>
#include <QtWidgets/QGraphicsView>
#include <QtWidgets/QGridLayout>
#include <QtWidgets/QGroupBox>
#include <QtWidgets/QHBoxLayout>
#include <QtWidgets/QInputDialog>
#include <QtCore/QIdentityProxyModel>
#include <QtGui/QIcon>
#include <QtGui/QImage>
#include <QtCore/QItemSelectionModel>
#include <QtCore/QIODevice>
#include <QtCore/QJsonArray>
#include <QtCore/QJsonDocument>
#include <QtCore/QJsonObject>
#include <QtCore/QJsonParseError>
#include <QtCore/QJsonValue>
#include <QtCore/QHash>
#include <QtCore/QMap>
#include <QtGui/QKeySequence>
#include <QtWidgets/QKeySequenceEdit>
#include <QtWidgets/QLabel>
#include <QtCore/QLine>
#include <QtCore/QLineF>
#include <QtWidgets/QLCDNumber>
#include <QtCore/QLibrary>
#include <QtWidgets/QLineEdit>
#include <QtWidgets/QListView>
#include <QtWidgets/QListWidget>
#include <QtWidgets/QMainWindow>
#include <QtCore/QMargins>
#include <QtCore/QMarginsF>
#include <QtWidgets/QMenu>
#include <QtWidgets/QMenuBar>
#include <QtCore/QMetaMethod>
#include <QtCore/QMetaEnum>
#include <QtCore/QMetaProperty>
#include <QtWidgets/QMdiArea>
#include <QtWidgets/QMessageBox>
#include <QtGui/QMovie>
#include <QtNetwork/QHostAddress>
#include <QtNetwork/QNetworkAccessManager>
#include <QtNetwork/QNetworkReply>
#include <QtNetwork/QNetworkRequest>
#include <QtPositioning/QGeoCoordinate>
#include <QtCore/QPluginLoader>
#include <QtCore/QProcess>
#include <QtCore/QRegularExpression>
#include <QtCore/QPointer>
#include <QtWidgets/QPlainTextEdit>
#include <QtCore/QPoint>
#include <QtCore/QPointF>
#include <QtWidgets/QProgressBar>
#include <QtWidgets/QProgressDialog>
#include <QtWidgets/QPushButton>
#include <QtGui/QPixmap>
#include <QtWidgets/QRadioButton>
#include <QtCore/QRect>
#include <QtCore/QRectF>
#include <QtWidgets/QScrollArea>
#include <QtWidgets/QScrollBar>
#include <QtCore/QSaveFile>
#include <QtCore/QSet>
#include <QtCore/QSettings>
#include <QtGui/QShortcut>
#include <QtTest/QSignalSpy>
#include <QtCore/QSignalMapper>
#include <QtCore/QSize>
#include <QtCore/QSizeF>
#include <QtWidgets/QSlider>
#include <QtCore/QSortFilterProxyModel>
#include <QtWidgets/QSpinBox>
#include <QtWidgets/QSplitter>
#include <QtWidgets/QStackedWidget>
#include <QtGui/QStandardItemModel>
#include <QtWidgets/QStatusBar>
#include <QtCore/QStringListModel>
#include <QtWidgets/QSystemTrayIcon>
#include <QtWidgets/QTabBar>
#include <QtWidgets/QTabWidget>
#include <QtWidgets/QTableView>
#include <QtWidgets/QTableWidget>
#include <QtCore/QTime>
#include <QtWidgets/QTimeEdit>
#include <QtCore/QTemporaryFile>
#include <QtCore/QThread>
#include <QtCore/QThreadPool>
#include <QtWidgets/QTextEdit>
#include <QtGui/QTextDocument>
#include <QtCore/QTimer>
#include <QtWidgets/QToolBar>
#include <QtWidgets/QToolButton>
#include <QtCore/QTranslator>
#include <QtWidgets/QTreeView>
#include <QtWidgets/QTreeWidget>
#include <QtNetwork/QTcpServer>
#include <QtGui/QUndoGroup>
#include <QtGui/QUndoStack>
#include <QtCore/QUrl>
#include <QtCore/QUuid>
#include <QtGui/QVector2D>
#include <QtGui/QVector3D>
#include <QtGui/QVector4D>
#include <QtWidgets/QVBoxLayout>
#include <QtWidgets/QWidget>
#include <QtWidgets/QWizard>
#include <QtWidgets/QWizardPage>
#if __has_include(<QtAxContainer/QAxObject>)
#include <QtAxContainer/QAxObject>
#include <QtAxContainer/QAxWidget>
#include <QtAxContainer/QAxBase>
#define KRY_QT6_HAS_AXCONTAINER 1
#endif
#include <QtWidgets/QGraphicsScene>
#include <QtWidgets/QGraphicsProxyWidget>
#include <QtWidgets/QTextBrowser>
#include <QtWidgets/QUndoView>
#include <QtCore/QConcatenateTablesProxyModel>
#include <QtCore/QTransposeProxyModel>
#include <QtCore/QBuffer>

#include <algorithm>
#include <array>
#include <cmath>
#include <cstdint>
#include <cstring>
#include <map>
#include <memory>
#include <limits>
#include <mutex>
#include <optional>
#include <string>
#include <vector>

#include "generated_default_constructors.h"
#include "generated_constructors/qt6_constructors.h"
#include "generated_methods/qt6_methods.h"
#include "generated_value_methods/qt6_value_methods.h"
#include "generated_value_constructors/qt6_value_constructors.h"
#include "qt6_direct.h"

namespace {

constexpr int32_t kMaxRequestBytes = 16 * 1024 * 1024;
constexpr int32_t kMaxResponseBytes = 16 * 1024 * 1024;
constexpr int kMaxInvocationArguments = 10;
constexpr int kMaxEventsPerPoll = 1000;
constexpr int kMaxWaitMilliseconds = 30000;

struct Subscription {
    qint64 objectId;
    QByteArray signature;
    std::unique_ptr<QSignalSpy> spy;
};

std::mutex stateMutex;
std::unique_ptr<QApplication> application;
QThread *applicationThread = nullptr;
int applicationArgc = 1;
char applicationName[] = "kryndel";
char *applicationArgv[] = {applicationName, nullptr};
std::map<qint64, QPointer<QObject>> objects;
std::map<qint64, QVariant> values;
std::map<qint64, std::vector<std::unique_ptr<Subscription>>> subscriptions;
qint64 nextObjectId = 1;

QJsonObject success(const QJsonValue &value = QJsonValue(QJsonValue::Null)) {
    return QJsonObject{{QStringLiteral("ok"), true}, {QStringLiteral("value"), value}};
}

QJsonObject failure(const QString &message) {
    return QJsonObject{{QStringLiteral("ok"), false}, {QStringLiteral("error"), message}};
}

QObject *objectForId(qint64 id) {
    auto it = objects.find(id);
    if (it == objects.end() || it->second.isNull()) {
        return nullptr;
    }
    return it->second.data();
}

qint64 idForObject(const QObject *object) {
    for (const auto &entry : objects) {
        if (entry.second.data() == object) {
            return entry.first;
        }
    }
    return 0;
}

qint64 registerObject(QObject *object) {
    if (object == nullptr) return 0;
    const qint64 existing = idForObject(object);
    if (existing != 0) return existing;
    const qint64 id = nextObjectId++;
    objects.emplace(id, QPointer<QObject>(object));
    return id;
}

qint64 registerValue(const QVariant &value) {
    if (!value.isValid()) return 0;
    const qint64 id = nextObjectId++;
    values.emplace(id, value);
    return id;
}

QVariant *valueForId(qint64 id) {
    const auto it = values.find(id);
    return it == values.end() ? nullptr : &it->second;
}

QJsonObject objectHandleValue(QObject *object) {
    return QJsonObject{{QStringLiteral("object_id"), static_cast<double>(registerObject(object))}};
}

bool isDescendantOf(const QObject *candidate, const QObject *ancestor) {
    for (const QObject *parent = candidate == nullptr ? nullptr : candidate->parent();
         parent != nullptr; parent = parent->parent()) {
        if (parent == ancestor) return true;
    }
    return false;
}

bool parseObjectId(const QJsonValue &value, qint64 *id, QString *error) {
    if (!value.isDouble()) {
        *error = QStringLiteral("object_id must be an integer handle");
        return false;
    }
    const double raw = value.toDouble();
    if (!std::isfinite(raw) || std::floor(raw) != raw || raw < 1 || raw > 9007199254740991.0) {
        *error = QStringLiteral("object_id is outside the supported range");
        return false;
    }
    *id = static_cast<qint64>(raw);
    return true;
}

QObject *objectArgument(const QJsonArray &arguments, qsizetype index, QString *error) {
    if (index < 0 || index >= arguments.size() || !arguments.at(index).isObject()) {
        *error = QStringLiteral("Qt object arguments must be {\"object_id\": handle}");
        return nullptr;
    }
    qint64 id = 0;
    if (!parseObjectId(arguments.at(index).toObject().value(QStringLiteral("object_id")), &id, error)) {
        return nullptr;
    }
    QObject *object = objectForId(id);
    if (object == nullptr) *error = QStringLiteral("Qt object handle is closed or unknown");
    return object;
}

bool readNumber(const QJsonObject &object, const QString &key, double *number, QString *error) {
    const QJsonValue value = object.value(key);
    if (!value.isDouble() || !std::isfinite(value.toDouble())) {
        *error = QStringLiteral("Qt value field %1 must be a finite JSON number").arg(key);
        return false;
    }
    *number = value.toDouble();
    return true;
}

bool readInteger(const QJsonObject &object, const QString &key, int *number, QString *error) {
    double value = 0;
    if (!readNumber(object, key, &value, error)) return false;
    if (std::floor(value) != value || value < std::numeric_limits<int>::min() ||
        value > std::numeric_limits<int>::max()) {
        *error = QStringLiteral("Qt value field %1 must be a 32-bit integer").arg(key);
        return false;
    }
    *number = static_cast<int>(value);
    return true;
}

bool readJsonInteger(const QJsonValue &value, int *number) {
    if (!value.isDouble()) return false;
    const double raw = value.toDouble();
    if (!std::isfinite(raw) || std::floor(raw) != raw ||
        raw < std::numeric_limits<int>::min() || raw > std::numeric_limits<int>::max()) return false;
    *number = static_cast<int>(raw);
    return true;
}

QJsonObject pointObject(double x, double y) {
    return QJsonObject{{QStringLiteral("x"), x}, {QStringLiteral("y"), y}};
}

QJsonObject sizeObject(double width, double height) {
    return QJsonObject{{QStringLiteral("width"), width}, {QStringLiteral("height"), height}};
}

QJsonObject rectObject(double x, double y, double width, double height) {
    return QJsonObject{{QStringLiteral("x"), x}, {QStringLiteral("y"), y},
                       {QStringLiteral("width"), width}, {QStringLiteral("height"), height}};
}

QJsonObject marginsObject(double left, double top, double right, double bottom) {
    return QJsonObject{{QStringLiteral("left"), left}, {QStringLiteral("top"), top},
                       {QStringLiteral("right"), right}, {QStringLiteral("bottom"), bottom}};
}

QByteArray pngBase64(const QImage &image) {
    QByteArray encoded;
    QBuffer buffer(&encoded);
    if (!buffer.open(QIODevice::WriteOnly) || !image.save(&buffer, "PNG")) return {};
    return encoded.toBase64();
}

bool decodePng(const QJsonValue &value, QImage *image, QString *error) {
    QByteArray bytes;
    if (value.isString()) {
        if (!image->load(value.toString())) {
            *error = QStringLiteral("Qt could not load image file %1").arg(value.toString());
            return false;
        }
        return true;
    }
    if (!value.isObject() || !value.toObject().value(QStringLiteral("png_base64")).isString()) {
        *error = QStringLiteral("image values must be a file path or {\"png_base64\": \"...\"}");
        return false;
    }
    const QByteArray::FromBase64Result decoded = QByteArray::fromBase64Encoding(
        value.toObject().value(QStringLiteral("png_base64")).toString().toLatin1());
    if (decoded.decodingStatus != QByteArray::Base64DecodingStatus::Ok) {
        *error = QStringLiteral("image png_base64 data is malformed");
        return false;
    }
    bytes = decoded.decoded;
    *image = QImage::fromData(bytes);
    if (image->isNull()) {
        *error = QStringLiteral("Qt could not decode the supplied image data");
        return false;
    }
    return true;
}

bool hasNul(const QString &value) {
    return value.contains(QChar::Null);
}

QMetaEnum enumForMetaType(const QMetaType &type) {
    const QMetaObject *metaObject = type.metaObject();
    if (metaObject == nullptr || type.name() == nullptr) return {};
    QString typeName = QString::fromLatin1(type.name());
    if (typeName.startsWith(QStringLiteral("QFlags<")) && typeName.endsWith(QLatin1Char('>'))) {
        typeName = typeName.mid(7, typeName.size() - 8);
    }
    const qsizetype separator = typeName.lastIndexOf(QStringLiteral("::"));
    const QString simpleName = separator >= 0 ? typeName.mid(separator + 2) : typeName;
    for (int i = 0; i < metaObject->enumeratorCount(); ++i) {
        const QMetaEnum enumeration = metaObject->enumerator(i);
        if (simpleName == QLatin1String(enumeration.name())) return enumeration;
    }
    return {};
}

QVariant fromJson(const QJsonValue &value, const QMetaType &target, QString *error) {
    if (!target.isValid()) {
        *error = QStringLiteral("Qt does not expose a meta-type for this argument");
        return {};
    }
    if (target == QMetaType::fromType<QVariant>()) return value.toVariant();

    QVariant converted;
    if (target.flags().testFlag(QMetaType::PointerToQObject)) {
        const QMetaObject *targetClass = target.metaObject();
        if (targetClass == nullptr) {
            *error = QStringLiteral("Qt does not expose a class meta-object for pointer type %1")
                         .arg(QString::fromLatin1(target.name()));
            return {};
        }
        QObject *nativeObject = nullptr;
        if (!value.isNull()) {
            if (!value.isObject()) {
                *error = QStringLiteral("QObject arguments must be null or {\"object_id\": handle}");
                return {};
            }
            qint64 id = 0;
            if (!parseObjectId(value.toObject().value(QStringLiteral("object_id")), &id, error)) {
                return {};
            }
            nativeObject = objectForId(id);
            if (nativeObject == nullptr) {
                *error = QStringLiteral("QObject handle is closed or unknown");
                return {};
            }
            if (targetClass->cast(nativeObject) == nullptr) {
                *error = QStringLiteral("QObject handle is not compatible with argument type %1")
                             .arg(QString::fromLatin1(target.name()));
                return {};
            }
        }
        void *typedPointer = nativeObject == nullptr ? nullptr : targetClass->cast(nativeObject);
        converted = QVariant(target, &typedPointer);
    } else if (value.isObject() && value.toObject().contains(QStringLiteral("value_id"))) {
        const QJsonValue rawIdValue = value.toObject().value(QStringLiteral("value_id"));
        qint64 id = 0;
        if (!rawIdValue.isDouble() || !std::isfinite(rawIdValue.toDouble()) ||
            std::floor(rawIdValue.toDouble()) != rawIdValue.toDouble() || rawIdValue.toDouble() < 1 ||
            rawIdValue.toDouble() > 9007199254740991.0) {
            *error = QStringLiteral("Qt value handle must be a positive integer value_id");
            return {};
        }
        id = static_cast<qint64>(rawIdValue.toDouble());
        const QVariant *stored = valueForId(id);
        if (stored == nullptr) {
            *error = QStringLiteral("Qt value handle is closed or unknown");
            return {};
        }
        converted = *stored;
        if (converted.metaType() != target && !converted.convert(target)) {
            *error = QStringLiteral("Qt value type %1 cannot be converted to %2")
                         .arg(QString::fromLatin1(converted.metaType().name()), QString::fromLatin1(target.name()));
            return {};
        }
    } else if (target.flags().testFlag(QMetaType::IsEnumeration)) {
        quint64 rawBits = 0;
        if (value.isString()) {
            const QMetaEnum enumeration = enumForMetaType(target);
            if (!enumeration.isValid()) {
                *error = QStringLiteral("Qt enum metadata is unavailable for type %1; pass its integer value")
                             .arg(QString::fromLatin1(target.name()));
                return {};
            }
            QByteArray key = value.toString().toLatin1();
            key.replace(" ", "");
            const std::optional<quint64> parsed = enumeration.isFlag()
                                                        ? enumeration.keysToValue64(key.constData())
                                                        : enumeration.keyToValue64(key.constData());
            if (!parsed.has_value()) {
                *error = QStringLiteral("unknown key %1 for Qt enum %2")
                             .arg(value.toString(), QString::fromLatin1(target.name()));
                return {};
            }
            rawBits = *parsed;
        } else if (value.isDouble()) {
            const double raw = value.toDouble();
            if (!std::isfinite(raw) || std::floor(raw) != raw || std::abs(raw) > 9007199254740991.0) {
                *error = QStringLiteral("Qt enum values must be finite, integral JSON numbers within the exact integer range");
                return {};
            }
            rawBits = static_cast<quint64>(static_cast<qint64>(raw));
        } else {
            *error = QStringLiteral("Qt enum values must be a key string or integer");
            return {};
        }
        switch (target.sizeOf()) {
        case 1: { const quint8 raw = static_cast<quint8>(rawBits); converted = QVariant(target, &raw); break; }
        case 2: { const quint16 raw = static_cast<quint16>(rawBits); converted = QVariant(target, &raw); break; }
        case 4: { const quint32 raw = static_cast<quint32>(rawBits); converted = QVariant(target, &raw); break; }
        case 8: { const quint64 raw = rawBits; converted = QVariant(target, &raw); break; }
        default:
            *error = QStringLiteral("unsupported storage size for Qt enum type %1").arg(QString::fromLatin1(target.name()));
            return {};
        }
    } else if (target == QMetaType::fromType<QVariantMap>() ||
               target == QMetaType::fromType<QVariantHash>()) {
        if (!value.isObject()) {
            *error = QStringLiteral("Qt variant maps require a JSON object");
            return {};
        }
        const QJsonObject object = value.toObject();
        if (target == QMetaType::fromType<QVariantMap>()) {
            QVariantMap map;
            for (auto entry = object.constBegin(); entry != object.constEnd(); ++entry) {
                map.insert(entry.key(), entry.value().toVariant());
            }
            converted = QVariant::fromValue(map);
        } else {
            QVariantHash map;
            for (auto entry = object.constBegin(); entry != object.constEnd(); ++entry) {
                map.insert(entry.key(), entry.value().toVariant());
            }
            converted = QVariant::fromValue(map);
        }
    } else if (target == QMetaType::fromType<QMap<QString, QString>>() ||
               target == QMetaType::fromType<QHash<QString, QString>>()) {
        if (!value.isObject()) {
            *error = QStringLiteral("Qt string maps require a JSON object of string values");
            return {};
        }
        const QJsonObject object = value.toObject();
        if (target == QMetaType::fromType<QMap<QString, QString>>()) {
            QMap<QString, QString> map;
            for (auto entry = object.constBegin(); entry != object.constEnd(); ++entry) {
                if (!entry.value().isString()) {
                    *error = QStringLiteral("Qt string map values must be strings");
                    return {};
                }
                map.insert(entry.key(), entry.value().toString());
            }
            converted = QVariant::fromValue(map);
        } else {
            QHash<QString, QString> map;
            for (auto entry = object.constBegin(); entry != object.constEnd(); ++entry) {
                if (!entry.value().isString()) {
                    *error = QStringLiteral("Qt string map values must be strings");
                    return {};
                }
                map.insert(entry.key(), entry.value().toString());
            }
            converted = QVariant::fromValue(map);
        }
    } else if (target == QMetaType::fromType<QJsonValue>()) {
        converted = QVariant::fromValue(value);
    } else if (target == QMetaType::fromType<QJsonObject>()) {
        if (!value.isObject()) { *error = QStringLiteral("QJsonObject arguments require a JSON object"); return {}; }
        converted = QVariant::fromValue(value.toObject());
    } else if (target == QMetaType::fromType<QJsonArray>()) {
        if (!value.isArray()) { *error = QStringLiteral("QJsonArray arguments require a JSON array"); return {}; }
        converted = QVariant::fromValue(value.toArray());
    } else if (target == QMetaType::fromType<QPoint>() || target == QMetaType::fromType<QPointF>() ||
               target == QMetaType::fromType<QSize>() || target == QMetaType::fromType<QSizeF>() ||
               target == QMetaType::fromType<QRect>() || target == QMetaType::fromType<QRectF>() ||
               target == QMetaType::fromType<QMargins>() || target == QMetaType::fromType<QMarginsF>()) {
        if (!value.isObject()) {
            *error = QStringLiteral("Qt geometry values must be JSON objects");
            return {};
        }
        const QJsonObject object = value.toObject();
        const bool integral = target == QMetaType::fromType<QPoint>() || target == QMetaType::fromType<QSize>() ||
                              target == QMetaType::fromType<QRect>() || target == QMetaType::fromType<QMargins>();
        double x = 0, y = 0, width = 0, height = 0, left = 0, top = 0, right = 0, bottom = 0;
        auto field = [&](const QString &name, double *out) {
            return integral ? ([&]() { int n = 0; if (!readInteger(object, name, &n, error)) return false; *out = n; return true; })()
                            : readNumber(object, name, out, error);
        };
        if (target == QMetaType::fromType<QPoint>() || target == QMetaType::fromType<QPointF>()) {
            if (!field(QStringLiteral("x"), &x) || !field(QStringLiteral("y"), &y)) return {};
            converted = target == QMetaType::fromType<QPoint>()
                            ? QVariant::fromValue(QPoint(static_cast<int>(x), static_cast<int>(y)))
                            : QVariant::fromValue(QPointF(x, y));
        } else if (target == QMetaType::fromType<QSize>() || target == QMetaType::fromType<QSizeF>()) {
            if (!field(QStringLiteral("width"), &width) || !field(QStringLiteral("height"), &height)) return {};
            converted = target == QMetaType::fromType<QSize>()
                            ? QVariant::fromValue(QSize(static_cast<int>(width), static_cast<int>(height)))
                            : QVariant::fromValue(QSizeF(width, height));
        } else if (target == QMetaType::fromType<QRect>() || target == QMetaType::fromType<QRectF>()) {
            if (!field(QStringLiteral("x"), &x) || !field(QStringLiteral("y"), &y) ||
                !field(QStringLiteral("width"), &width) || !field(QStringLiteral("height"), &height)) return {};
            converted = target == QMetaType::fromType<QRect>()
                            ? QVariant::fromValue(QRect(static_cast<int>(x), static_cast<int>(y),
                                                        static_cast<int>(width), static_cast<int>(height)))
                            : QVariant::fromValue(QRectF(x, y, width, height));
        } else {
            if (!field(QStringLiteral("left"), &left) || !field(QStringLiteral("top"), &top) ||
                !field(QStringLiteral("right"), &right) || !field(QStringLiteral("bottom"), &bottom)) return {};
            converted = target == QMetaType::fromType<QMargins>()
                            ? QVariant::fromValue(QMargins(static_cast<int>(left), static_cast<int>(top),
                                                           static_cast<int>(right), static_cast<int>(bottom)))
                            : QVariant::fromValue(QMarginsF(left, top, right, bottom));
        }
    } else if (target == QMetaType::fromType<QLine>() || target == QMetaType::fromType<QLineF>()) {
        if (!value.isObject()) { *error = QStringLiteral("QLine values must be JSON objects with x1, y1, x2, and y2 fields"); return {}; }
        const QJsonObject object = value.toObject();
        double x1 = 0, y1 = 0, x2 = 0, y2 = 0;
        const bool integral = target == QMetaType::fromType<QLine>();
        auto field = [&](const QString &name, double *out) {
            if (integral) { int n = 0; if (!readInteger(object, name, &n, error)) return false; *out = n; return true; }
            return readNumber(object, name, out, error);
        };
        if (!field(QStringLiteral("x1"), &x1) || !field(QStringLiteral("y1"), &y1) ||
            !field(QStringLiteral("x2"), &x2) || !field(QStringLiteral("y2"), &y2)) return {};
        converted = target == QMetaType::fromType<QLine>()
                        ? QVariant::fromValue(QLine(static_cast<int>(x1), static_cast<int>(y1), static_cast<int>(x2), static_cast<int>(y2)))
                        : QVariant::fromValue(QLineF(x1, y1, x2, y2));
    } else if (target == QMetaType::fromType<QVector2D>() || target == QMetaType::fromType<QVector3D>() ||
               target == QMetaType::fromType<QVector4D>()) {
        if (!value.isObject()) { *error = QStringLiteral("Qt vector values must be JSON objects with x, y, z, and optionally w fields"); return {}; }
        const QJsonObject object = value.toObject();
        double x = 0, y = 0, z = 0, w = 0;
        if (!readNumber(object, QStringLiteral("x"), &x, error) ||
            !readNumber(object, QStringLiteral("y"), &y, error)) return {};
        if (target != QMetaType::fromType<QVector2D>() && !readNumber(object, QStringLiteral("z"), &z, error)) return {};
        if (target == QMetaType::fromType<QVector4D>() && !readNumber(object, QStringLiteral("w"), &w, error)) return {};
        constexpr double maximumFloat = std::numeric_limits<float>::max();
        if (std::abs(x) > maximumFloat || std::abs(y) > maximumFloat || std::abs(z) > maximumFloat || std::abs(w) > maximumFloat) {
            *error = QStringLiteral("Qt vector values must fit in a finite 32-bit float");
            return {};
        }
        if (target == QMetaType::fromType<QVector2D>()) converted = QVariant::fromValue(QVector2D(static_cast<float>(x), static_cast<float>(y)));
        else if (target == QMetaType::fromType<QVector3D>()) converted = QVariant::fromValue(QVector3D(static_cast<float>(x), static_cast<float>(y), static_cast<float>(z)));
        else converted = QVariant::fromValue(QVector4D(static_cast<float>(x), static_cast<float>(y), static_cast<float>(z), static_cast<float>(w)));
    } else if (target == QMetaType::fromType<QUuid>()) {
        if (!value.isString()) { *error = QStringLiteral("QUuid values must use UUID string format"); return {}; }
        QString uuidText = value.toString();
        const bool opensBrace = uuidText.startsWith(QLatin1Char('{'));
        const bool closesBrace = uuidText.endsWith(QLatin1Char('}'));
        if (opensBrace != closesBrace) { *error = QStringLiteral("invalid UUID brace format"); return {}; }
        if (opensBrace) uuidText = uuidText.mid(1, uuidText.size() - 2);
        static const QRegularExpression uuidFormat(QStringLiteral("^(?:[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}|[0-9A-Fa-f]{32})$"));
        if (!uuidFormat.match(uuidText).hasMatch()) { *error = QStringLiteral("invalid UUID string"); return {}; }
        converted = QVariant::fromValue(QUuid(uuidText));
    } else if (target == QMetaType::fromType<QRegularExpression>()) {
        QString pattern;
        int options = 0;
        if (value.isString()) {
            pattern = value.toString();
        } else if (value.isObject() && value.toObject().value(QStringLiteral("pattern")).isString()) {
            const QJsonObject object = value.toObject();
            pattern = object.value(QStringLiteral("pattern")).toString();
            const QJsonValue optionValue = object.value(QStringLiteral("options"));
            if (!optionValue.isUndefined() && !readJsonInteger(optionValue, &options)) {
                *error = QStringLiteral("QRegularExpression options must be a 32-bit integer bitmask");
                return {};
            }
        } else {
            *error = QStringLiteral("QRegularExpression values must be a pattern string or {\"pattern\": string, \"options\": integer}");
            return {};
        }
        const auto patternOptions = static_cast<QRegularExpression::PatternOptions>(options);
        const QRegularExpression expression(pattern, patternOptions);
        if (!expression.isValid()) { *error = QStringLiteral("invalid QRegularExpression: %1").arg(expression.errorString()); return {}; }
        converted = QVariant::fromValue(expression);
    } else if (target == QMetaType::fromType<QGeoCoordinate>()) {
        if (!value.isObject()) {
            *error = QStringLiteral("QGeoCoordinate values require a JSON object with latitude and longitude");
            return {};
        }
        const QJsonObject object = value.toObject();
        double latitude = 0;
        double longitude = 0;
        if (!readNumber(object, QStringLiteral("latitude"), &latitude, error) ||
            !readNumber(object, QStringLiteral("longitude"), &longitude, error)) {
            return {};
        }
        QGeoCoordinate coordinate;
        const QJsonValue altitudeValue = object.value(QStringLiteral("altitude"));
        if (altitudeValue.isUndefined() || altitudeValue.isNull()) {
            coordinate = QGeoCoordinate(latitude, longitude);
        } else {
            double altitude = 0;
            if (!altitudeValue.isDouble() || !std::isfinite(altitudeValue.toDouble())) {
                *error = QStringLiteral("QGeoCoordinate altitude must be a finite number or null");
                return {};
            }
            altitude = altitudeValue.toDouble();
            coordinate = QGeoCoordinate(latitude, longitude, altitude);
        }
        if (!coordinate.isValid()) {
            *error = QStringLiteral("QGeoCoordinate latitude or longitude is outside its valid range");
            return {};
        }
        converted = QVariant::fromValue(coordinate);
    } else if (target == QMetaType::fromType<QHostAddress>()) {
        if (!value.isString()) { *error = QStringLiteral("QHostAddress values must be IPv4 or IPv6 address strings"); return {}; }
        QHostAddress address;
        if (!address.setAddress(value.toString())) { *error = QStringLiteral("invalid IPv4 or IPv6 host address"); return {}; }
        converted = QVariant::fromValue(address);
    } else if (target == QMetaType::fromType<QNetworkRequest>()) {
        QString urlText;
        QJsonObject headers;
        if (value.isString()) {
            urlText = value.toString();
        } else if (value.isObject() && value.toObject().value(QStringLiteral("url")).isString()) {
            const QJsonObject object = value.toObject();
            urlText = object.value(QStringLiteral("url")).toString();
            const QJsonValue headersValue = object.value(QStringLiteral("headers"));
            if (!headersValue.isUndefined() && !headersValue.isObject()) {
                *error = QStringLiteral("QNetworkRequest headers must be a JSON object of string values");
                return {};
            }
            headers = headersValue.toObject();
        } else {
            *error = QStringLiteral("QNetworkRequest values must be a URL string or {\"url\": string, \"headers\": object}");
            return {};
        }
        const QUrl url(urlText, QUrl::TolerantMode);
        if (!url.isValid()) { *error = QStringLiteral("invalid QNetworkRequest URL: %1").arg(url.errorString()); return {}; }
        QNetworkRequest request(url);
        for (auto it = headers.constBegin(); it != headers.constEnd(); ++it) {
            if (!it.value().isString()) { *error = QStringLiteral("QNetworkRequest header values must be strings"); return {}; }
            const QByteArray name = it.key().toLatin1();
            const QByteArray content = it.value().toString().toLatin1();
            if (name.isEmpty() || name.contains('\r') || name.contains('\n') || name.contains('\0') ||
                content.contains('\r') || content.contains('\n') || content.contains('\0')) {
                *error = QStringLiteral("QNetworkRequest header names and values must not contain CR, LF, or NUL");
                return {};
            }
            request.setRawHeader(name, content);
        }
        converted = QVariant::fromValue(request);
    } else if (target == QMetaType::fromType<QImage>() || target == QMetaType::fromType<QPixmap>() ||
               target == QMetaType::fromType<QIcon>()) {
        QImage image;
        if (!decodePng(value, &image, error)) return {};
        if (target == QMetaType::fromType<QImage>()) converted = QVariant::fromValue(image);
        else if (target == QMetaType::fromType<QPixmap>()) converted = QVariant::fromValue(QPixmap::fromImage(image));
        else converted = QVariant::fromValue(QIcon(QPixmap::fromImage(image)));
    } else if (target == QMetaType::fromType<QColor>()) {
        if (!value.isString()) {
            *error = QStringLiteral("QColor values must be a color name or #RRGGBB/#AARRGGBB string");
            return {};
        }
        const QColor color = QColor::fromString(value.toString());
        if (!color.isValid()) {
            *error = QStringLiteral("invalid QColor name or hexadecimal color string");
            return {};
        }
        converted = QVariant::fromValue(color);
    } else if (target == QMetaType::fromType<QDate>()) {
        if (!value.isString()) { *error = QStringLiteral("QDate values must use ISO date strings"); return {}; }
        const QDate date = QDate::fromString(value.toString(), Qt::ISODate);
        if (!date.isValid()) { *error = QStringLiteral("invalid ISO QDate value"); return {}; }
        converted = QVariant::fromValue(date);
    } else if (target == QMetaType::fromType<QTime>()) {
        if (!value.isString()) { *error = QStringLiteral("QTime values must use HH:mm:ss[.zzz] strings"); return {}; }
        QTime time = QTime::fromString(value.toString(), QStringLiteral("HH:mm:ss.zzz"));
        if (!time.isValid()) time = QTime::fromString(value.toString(), QStringLiteral("HH:mm:ss"));
        if (!time.isValid()) { *error = QStringLiteral("invalid ISO QTime value"); return {}; }
        converted = QVariant::fromValue(time);
    } else if (target == QMetaType::fromType<QDateTime>()) {
        if (!value.isString()) { *error = QStringLiteral("QDateTime values must use ISO date-time strings"); return {}; }
        QDateTime dateTime = QDateTime::fromString(value.toString(), Qt::ISODateWithMs);
        if (!dateTime.isValid()) dateTime = QDateTime::fromString(value.toString(), Qt::ISODate);
        if (!dateTime.isValid()) { *error = QStringLiteral("invalid ISO QDateTime value"); return {}; }
        converted = QVariant::fromValue(dateTime);
    } else if (target == QMetaType::fromType<QUrl>()) {
        if (!value.isString()) { *error = QStringLiteral("QUrl values must be strings"); return {}; }
        const QUrl url(value.toString(), QUrl::TolerantMode);
        if (!url.isValid()) { *error = QStringLiteral("invalid QUrl: %1").arg(url.errorString()); return {}; }
        converted = QVariant::fromValue(url);
    } else if (target == QMetaType::fromType<QFont>()) {
        if (!value.isString()) { *error = QStringLiteral("QFont values must use QFont::toString() format"); return {}; }
        QFont font;
        if (!font.fromString(value.toString())) { *error = QStringLiteral("invalid QFont serialized value"); return {}; }
        converted = QVariant::fromValue(font);
    } else if (target == QMetaType::fromType<QKeySequence>()) {
        if (!value.isString()) { *error = QStringLiteral("QKeySequence values must be PortableText strings"); return {}; }
        converted = QVariant::fromValue(QKeySequence::fromString(value.toString(), QKeySequence::PortableText));
    } else if (target == QMetaType::fromType<QByteArray>()) {
        if (!value.isObject() || !value.toObject().value(QStringLiteral("base64")).isString()) {
            *error = QStringLiteral("QByteArray values must be {\"base64\": \"...\"}");
            return {};
        }
        const QByteArray::FromBase64Result decoded = QByteArray::fromBase64Encoding(
            value.toObject().value(QStringLiteral("base64")).toString().toLatin1());
        if (decoded.decodingStatus != QByteArray::Base64DecodingStatus::Ok) {
            *error = QStringLiteral("QByteArray base64 data is malformed");
            return {};
        }
        converted = QVariant::fromValue(decoded.decoded);
    } else if (target == QMetaType::fromType<qlonglong>()) {
        qlonglong integer = 0;
        if (value.isDouble()) {
            const double raw = value.toDouble();
            constexpr double maximumExactJsonInteger = 9007199254740991.0;
            if (!std::isfinite(raw) || std::floor(raw) != raw ||
                raw < -maximumExactJsonInteger || raw > maximumExactJsonInteger) {
                *error = QStringLiteral("64-bit integer JSON numbers must be integral and within the exact range; use a decimal string for larger values");
                return {};
            }
            integer = static_cast<qlonglong>(raw);
        } else if (!value.isString()) {
            *error = QStringLiteral("64-bit integer arguments must be exact JSON numbers or decimal strings");
            return {};
        } else {
            bool valid = false;
            integer = value.toString().toLongLong(&valid, 10);
            if (!valid) {
                *error = QStringLiteral("invalid signed 64-bit decimal integer");
                return {};
            }
        }
        converted = QVariant::fromValue(integer);
    } else if (target == QMetaType::fromType<qulonglong>()) {
        qulonglong integer = 0;
        if (value.isDouble()) {
            const double raw = value.toDouble();
            constexpr double maximumExactJsonInteger = 9007199254740991.0;
            if (!std::isfinite(raw) || std::floor(raw) != raw || raw < 0 || raw > maximumExactJsonInteger) {
                *error = QStringLiteral("unsigned 64-bit integer JSON numbers must be integral and within the exact range; use a decimal string for larger values");
                return {};
            }
            integer = static_cast<qulonglong>(raw);
        } else if (!value.isString()) {
            *error = QStringLiteral("unsigned 64-bit integer arguments must be exact JSON numbers or decimal strings");
            return {};
        } else {
            bool valid = false;
            integer = value.toString().toULongLong(&valid, 10);
            if (!valid) {
                *error = QStringLiteral("invalid unsigned 64-bit decimal integer");
                return {};
            }
        }
        converted = QVariant::fromValue(integer);
    } else {
        if ((target.id() == QMetaType::Int || target.id() == QMetaType::UInt ||
             target.id() == QMetaType::LongLong || target.id() == QMetaType::ULongLong) &&
            value.isDouble()) {
            const double raw = value.toDouble();
            if (!std::isfinite(raw) || std::floor(raw) != raw) {
                *error = QStringLiteral("integer Qt arguments require a finite integral JSON number");
                return {};
            }
            if (target.id() == QMetaType::UInt &&
                (raw < 0 || raw > std::numeric_limits<unsigned int>::max())) {
                *error = QStringLiteral("unsigned Qt integer arguments must fit in 32 bits");
                return {};
            }
        }
        converted = value.toVariant();
    }

    if (converted.metaType() != target && !converted.convert(target)) {
        *error = QStringLiteral("JSON value cannot be converted to Qt type %1").arg(QString::fromLatin1(target.name()));
        return {};
    }
    return converted;
}

QJsonValue toJson(const QVariant &value) {
    if (!value.isValid()) {
        return QJsonValue(QJsonValue::Null);
    }
    if (value.metaType().flags().testFlag(QMetaType::PointerToQObject)) {
        QVariant objectValue = value;
        const QMetaType objectPointerType = QMetaType::fromType<QObject *>();
        if (objectValue.metaType() != objectPointerType && !objectValue.convert(objectPointerType)) {
            return QJsonValue(QJsonValue::Null);
        }
        QObject *object = objectValue.value<QObject *>();
        if (object == nullptr) {
            return QJsonValue(QJsonValue::Null);
        }
        const qint64 id = idForObject(object);
        if (id == 0) {
            return QJsonObject{{QStringLiteral("class"), QString::fromLatin1(object->metaObject()->className())},
                               {QStringLiteral("object_name"), object->objectName()}};
        }
        return QJsonObject{{QStringLiteral("object_id"), static_cast<double>(id)}};
    }
    const QMetaType type = value.metaType();
    if (type == QMetaType::fromType<QGeoCoordinate>()) {
        const QGeoCoordinate coordinate = value.value<QGeoCoordinate>();
        if (!coordinate.isValid()) return QJsonValue(QJsonValue::Null);
        QJsonObject object{{QStringLiteral("latitude"), coordinate.latitude()},
                           {QStringLiteral("longitude"), coordinate.longitude()}};
        if (std::isfinite(coordinate.altitude())) object.insert(QStringLiteral("altitude"), coordinate.altitude());
        return object;
    }
    if (type == QMetaType::fromType<QHostAddress>()) return value.value<QHostAddress>().toString();
    if (type == QMetaType::fromType<QNetworkRequest>()) {
        const QNetworkRequest request = value.value<QNetworkRequest>();
        QJsonObject headers;
        for (const QByteArray &name : request.rawHeaderList()) {
            headers.insert(QString::fromLatin1(name), QString::fromLatin1(request.rawHeader(name)));
        }
        return QJsonObject{{QStringLiteral("url"), request.url().toString(QUrl::FullyEncoded)},
                           {QStringLiteral("headers"), headers}};
    }
    if (value.metaType().flags().testFlag(QMetaType::IsGadget) && value.metaType().metaObject() != nullptr) {
        const qint64 id = registerValue(value);
        return QJsonObject{{QStringLiteral("value_id"), static_cast<double>(id)},
                           {QStringLiteral("type"), QString::fromLatin1(value.metaType().name())}};
    }
    if (value.metaType().flags().testFlag(QMetaType::IsEnumeration)) {
        const QMetaEnum enumeration = enumForMetaType(value.metaType());
        quint64 rawBits = 0;
        switch (value.metaType().sizeOf()) {
        case 1: {
            quint8 raw = 0; std::memcpy(&raw, value.constData(), sizeof(raw));
            if (enumeration.isValid() && !enumeration.isFlag()) return QJsonValue(static_cast<qint8>(raw));
            rawBits = raw;
            break;
        }
        case 2: {
            quint16 raw = 0; std::memcpy(&raw, value.constData(), sizeof(raw));
            if (enumeration.isValid() && !enumeration.isFlag()) return QJsonValue(static_cast<qint16>(raw));
            rawBits = raw;
            break;
        }
        case 4: {
            quint32 raw = 0; std::memcpy(&raw, value.constData(), sizeof(raw));
            if (enumeration.isValid() && !enumeration.isFlag()) return QJsonValue(static_cast<qint32>(raw));
            rawBits = raw;
            break;
        }
        case 8: std::memcpy(&rawBits, value.constData(), sizeof(rawBits)); break;
        default: return QJsonValue(QJsonValue::Null);
        }
        return QJsonValue(static_cast<double>(rawBits));
    }
    if (type == QMetaType::fromType<qlonglong>()) {
        const qlonglong integer = value.toLongLong();
        constexpr qlonglong maximumExactJsonInteger = 9007199254740991LL;
        if (integer >= -maximumExactJsonInteger && integer <= maximumExactJsonInteger) {
            return QJsonValue(static_cast<double>(integer));
        }
        return QJsonValue(QString::number(integer));
    }
    if (type == QMetaType::fromType<qulonglong>()) {
        const qulonglong integer = value.toULongLong();
        constexpr qulonglong maximumExactJsonInteger = 9007199254740991ULL;
        if (integer <= maximumExactJsonInteger) {
            return QJsonValue(static_cast<double>(integer));
        }
        return QJsonValue(QString::number(integer));
    }
    if (type == QMetaType::fromType<QJsonValue>()) return value.value<QJsonValue>();
    if (type == QMetaType::fromType<QJsonObject>()) return QJsonValue(value.value<QJsonObject>());
    if (type == QMetaType::fromType<QJsonArray>()) return QJsonValue(value.value<QJsonArray>());
    if (type == QMetaType::fromType<QVariantMap>()) {
        QJsonObject object;
        const QVariantMap map = value.value<QVariantMap>();
        for (auto entry = map.constBegin(); entry != map.constEnd(); ++entry) {
            object.insert(entry.key(), toJson(entry.value()));
        }
        return object;
    }
    if (type == QMetaType::fromType<QVariantHash>()) {
        QJsonObject object;
        const QVariantHash map = value.value<QVariantHash>();
        for (auto entry = map.constBegin(); entry != map.constEnd(); ++entry) {
            object.insert(entry.key(), toJson(entry.value()));
        }
        return object;
    }
    if (type == QMetaType::fromType<QMap<QString, QString>>()) {
        QJsonObject object;
        const QMap<QString, QString> map = value.value<QMap<QString, QString>>();
        for (auto entry = map.constBegin(); entry != map.constEnd(); ++entry) {
            object.insert(entry.key(), entry.value());
        }
        return object;
    }
    if (type == QMetaType::fromType<QHash<QString, QString>>()) {
        QJsonObject object;
        const QHash<QString, QString> map = value.value<QHash<QString, QString>>();
        for (auto entry = map.constBegin(); entry != map.constEnd(); ++entry) {
            object.insert(entry.key(), entry.value());
        }
        return object;
    }
    if (type == QMetaType::fromType<QPoint>()) {
        const QPoint point = value.value<QPoint>();
        return pointObject(point.x(), point.y());
    }
    if (type == QMetaType::fromType<QPointF>()) {
        const QPointF point = value.value<QPointF>();
        return pointObject(point.x(), point.y());
    }
    if (type == QMetaType::fromType<QSize>()) {
        const QSize size = value.value<QSize>();
        return sizeObject(size.width(), size.height());
    }
    if (type == QMetaType::fromType<QSizeF>()) {
        const QSizeF size = value.value<QSizeF>();
        return sizeObject(size.width(), size.height());
    }
    if (type == QMetaType::fromType<QRect>()) {
        const QRect rect = value.value<QRect>();
        return rectObject(rect.x(), rect.y(), rect.width(), rect.height());
    }
    if (type == QMetaType::fromType<QRectF>()) {
        const QRectF rect = value.value<QRectF>();
        return rectObject(rect.x(), rect.y(), rect.width(), rect.height());
    }
    if (type == QMetaType::fromType<QLine>()) {
        const QLine line = value.value<QLine>();
        return QJsonObject{{QStringLiteral("x1"), line.x1()}, {QStringLiteral("y1"), line.y1()},
                           {QStringLiteral("x2"), line.x2()}, {QStringLiteral("y2"), line.y2()}};
    }
    if (type == QMetaType::fromType<QLineF>()) {
        const QLineF line = value.value<QLineF>();
        return QJsonObject{{QStringLiteral("x1"), line.x1()}, {QStringLiteral("y1"), line.y1()},
                           {QStringLiteral("x2"), line.x2()}, {QStringLiteral("y2"), line.y2()}};
    }
    if (type == QMetaType::fromType<QVector2D>()) {
        const QVector2D vector = value.value<QVector2D>();
        return pointObject(vector.x(), vector.y());
    }
    if (type == QMetaType::fromType<QVector3D>()) {
        const QVector3D vector = value.value<QVector3D>();
        return QJsonObject{{QStringLiteral("x"), vector.x()}, {QStringLiteral("y"), vector.y()}, {QStringLiteral("z"), vector.z()}};
    }
    if (type == QMetaType::fromType<QVector4D>()) {
        const QVector4D vector = value.value<QVector4D>();
        return QJsonObject{{QStringLiteral("x"), vector.x()}, {QStringLiteral("y"), vector.y()},
                           {QStringLiteral("z"), vector.z()}, {QStringLiteral("w"), vector.w()}};
    }
    if (type == QMetaType::fromType<QUuid>()) return value.value<QUuid>().toString(QUuid::WithoutBraces);
    if (type == QMetaType::fromType<QRegularExpression>()) {
        const QRegularExpression expression = value.value<QRegularExpression>();
        return QJsonObject{{QStringLiteral("pattern"), expression.pattern()},
                           {QStringLiteral("options"), static_cast<int>(expression.patternOptions())}};
    }
    if (type == QMetaType::fromType<QImage>()) {
        const QByteArray encoded = pngBase64(value.value<QImage>());
        return encoded.isEmpty() ? QJsonValue(QJsonValue::Null)
                                 : QJsonValue(QJsonObject{{QStringLiteral("png_base64"), QString::fromLatin1(encoded)}});
    }
    if (type == QMetaType::fromType<QPixmap>()) {
        const QByteArray encoded = pngBase64(value.value<QPixmap>().toImage());
        return encoded.isEmpty() ? QJsonValue(QJsonValue::Null)
                                 : QJsonValue(QJsonObject{{QStringLiteral("png_base64"), QString::fromLatin1(encoded)}});
    }
    if (type == QMetaType::fromType<QIcon>()) {
        const QByteArray encoded = pngBase64(value.value<QIcon>().pixmap(64, 64).toImage());
        return encoded.isEmpty() ? QJsonValue(QJsonValue::Null)
                                 : QJsonValue(QJsonObject{{QStringLiteral("png_base64"), QString::fromLatin1(encoded)}});
    }
    if (type == QMetaType::fromType<QMargins>()) {
        const QMargins margins = value.value<QMargins>();
        return marginsObject(margins.left(), margins.top(), margins.right(), margins.bottom());
    }
    if (type == QMetaType::fromType<QMarginsF>()) {
        const QMarginsF margins = value.value<QMarginsF>();
        return marginsObject(margins.left(), margins.top(), margins.right(), margins.bottom());
    }
    if (type == QMetaType::fromType<QColor>()) return value.value<QColor>().name(QColor::HexArgb);
    if (type == QMetaType::fromType<QDate>()) return value.value<QDate>().toString(Qt::ISODate);
    if (type == QMetaType::fromType<QTime>()) return value.value<QTime>().toString(QStringLiteral("HH:mm:ss.zzz"));
    if (type == QMetaType::fromType<QDateTime>()) return value.value<QDateTime>().toString(Qt::ISODateWithMs);
    if (type == QMetaType::fromType<QUrl>()) return value.value<QUrl>().toString(QUrl::FullyEncoded);
    if (type == QMetaType::fromType<QFont>()) return value.value<QFont>().toString();
    if (type == QMetaType::fromType<QKeySequence>()) return value.value<QKeySequence>().toString(QKeySequence::PortableText);
    if (type == QMetaType::fromType<QByteArray>()) {
        return QJsonObject{{QStringLiteral("base64"), QString::fromLatin1(value.toByteArray().toBase64())}};
    }
    const QString className = QString::fromLatin1(type.name());
    if (kry_qt6_direct::availableDirectValueTypes().contains(className) ||
        kry_qt6_generated::availableDirectValueConstructors().contains(className)) {
        const qint64 id = registerValue(value);
        return QJsonObject{{QStringLiteral("value_id"), static_cast<double>(id)},
                           {QStringLiteral("type"), className}};
    }
    const QJsonValue json = QJsonValue::fromVariant(value);
    return json.isUndefined() ? QJsonValue(QJsonValue::Null) : json;
}

bool buildArguments(const QJsonArray &jsonArguments, const QList<QMetaType> &types,
                    QList<QVariant> *values, std::array<QGenericArgument, kMaxInvocationArguments> *arguments,
                    QString *error) {
    if (jsonArguments.size() != types.size()) {
        *error = QStringLiteral("expected %1 argument(s), got %2").arg(types.size()).arg(jsonArguments.size());
        return false;
    }
    if (jsonArguments.size() > kMaxInvocationArguments) {
        *error = QStringLiteral("this reflective call supports at most 10 arguments");
        return false;
    }
    values->reserve(jsonArguments.size());
    for (qsizetype i = 0; i < jsonArguments.size(); ++i) {
        QVariant converted = fromJson(jsonArguments.at(i), types.at(i), error);
        if (!converted.isValid() && types.at(i).id() != QMetaType::Nullptr &&
            types.at(i) != QMetaType::fromType<QVariant>()) {
            return false;
        }
        values->append(std::move(converted));
    }
    for (qsizetype i = 0; i < values->size(); ++i) {
        const void *argumentData = types.at(i) == QMetaType::fromType<QVariant>()
                                       ? static_cast<const void *>(&values->at(i))
                                       : values->at(i).constData();
        (*arguments)[static_cast<size_t>(i)] = QGenericArgument(types.at(i).name(), argumentData);
    }
    return true;
}

bool metaInherits(const QMetaObject *metaObject, const char *className) {
    for (const QMetaObject *current = metaObject; current != nullptr; current = current->superClass()) {
        if (qstrcmp(current->className(), className) == 0) return true;
    }
    return false;
}

QObject *constructInvokable(const QMetaObject *metaObject, QObject *parent,
                            const QJsonArray &jsonArguments, QString *error) {
    if (metaObject == nullptr) return nullptr;
    if (jsonArguments.size() > kMaxInvocationArguments) {
        *error = QStringLiteral("this reflective constructor supports at most 10 arguments");
        return nullptr;
    }

    QWidget *widgetParent = nullptr;
    QWindow *windowParent = nullptr;
    QLayout *existingLayout = nullptr;
    if (parent != nullptr && metaInherits(metaObject, "QWidget")) {
        widgetParent = qobject_cast<QWidget *>(parent);
        if (widgetParent == nullptr) { *error = QStringLiteral("widget constructors require a QWidget parent"); return nullptr; }
    }
    if (parent != nullptr && metaInherits(metaObject, "QWindow")) {
        windowParent = qobject_cast<QWindow *>(parent);
        if (windowParent == nullptr) { *error = QStringLiteral("window constructors require a QWindow parent"); return nullptr; }
    }
    if (parent != nullptr && metaInherits(metaObject, "QLayout")) {
        widgetParent = qobject_cast<QWidget *>(parent);
        if (widgetParent == nullptr) { *error = QStringLiteral("layout constructors require a QWidget parent"); return nullptr; }
        existingLayout = widgetParent->layout();
        if (existingLayout != nullptr) { *error = QStringLiteral("QWidget already has a layout"); return nullptr; }
    }
    Qt3DCore::QNode *nodeParent = nullptr;
    if (parent != nullptr && (metaInherits(metaObject, "QNode") || metaInherits(metaObject, "Qt3DCore::QNode"))) {
        nodeParent = qobject_cast<Qt3DCore::QNode *>(parent);
        if (nodeParent == nullptr) { *error = QStringLiteral("Qt 3D constructors require a Qt3DCore.QNode parent"); return nullptr; }
    }

    QStringList considered;
    for (int i = 0; i < metaObject->constructorCount(); ++i) {
        const QMetaMethod constructor = metaObject->constructor(i);
        if (constructor.access() != QMetaMethod::Public || constructor.parameterCount() != jsonArguments.size()) continue;
        QList<QMetaType> types;
        types.reserve(constructor.parameterCount());
        for (int parameter = 0; parameter < constructor.parameterCount(); ++parameter) {
            types.append(constructor.parameterMetaType(parameter));
        }
        QList<QVariant> values;
        std::array<QGenericArgument, kMaxInvocationArguments> arguments{};
        QString candidateError;
        if (!buildArguments(jsonArguments, types, &values, &arguments, &candidateError)) continue;
        considered.append(QString::fromLatin1(constructor.methodSignature()));
        QObject *created = metaObject->newInstance(
            arguments[0], arguments[1], arguments[2], arguments[3], arguments[4],
            arguments[5], arguments[6], arguments[7], arguments[8], arguments[9]);
        if (created == nullptr) continue;
        if (parent != nullptr && created->parent() == nullptr) {
            if (metaInherits(metaObject, "QWidget")) {
                qobject_cast<QWidget *>(created)->setParent(widgetParent);
            } else if (metaInherits(metaObject, "QWindow")) {
                qobject_cast<QWindow *>(created)->setParent(windowParent);
            } else if (metaInherits(metaObject, "QLayout")) {
                widgetParent->setLayout(qobject_cast<QLayout *>(created));
            } else if (nodeParent != nullptr) {
                qobject_cast<Qt3DCore::QNode *>(created)->setParent(nodeParent);
            } else {
                created->setParent(parent);
            }
        }
        return created;
    }
    *error = considered.isEmpty()
                 ? QStringLiteral("%1 exposes no public Q_INVOKABLE constructor with %2 compatible argument(s)")
                       .arg(QString::fromLatin1(metaObject->className())).arg(jsonArguments.size())
                 : QStringLiteral("Qt could not construct %1 with the matching invokable constructor(s): %2")
                       .arg(QString::fromLatin1(metaObject->className()), considered.join(QStringLiteral(", ")));
    return nullptr;
}

QObject *createObject(const QString &requestedClassName, QObject *parent, const QJsonArray &args, QString *error) {
    QString className = requestedClassName;
    QString moduleName;
    const qsizetype moduleSeparator = className.indexOf(QLatin1Char('.'));
    if (moduleSeparator >= 0) {
        moduleName = className.left(moduleSeparator);
        className = className.mid(moduleSeparator + 1);
        if (!kry_qt6_generated::hasClass(requestedClassName) &&
            !kry_qt6_generated::hasArgumentConstructor(moduleName, className)) {
            *error = QStringLiteral("Qt class %1 has no native constructor in this package").arg(requestedClassName);
            return nullptr;
        }
    } else {
        QSet<QString> availableModules;
        for (const kry_qt6_generated::Module &module : kry_qt6_generated::modules()) {
            if (module.hasClass(className) || module.metaObject(className) != nullptr) {
                availableModules.insert(QString::fromLatin1(module.name));
            }
        }
        for (const QString &candidateModule : kry_qt6_generated::argumentConstructorModules(className)) {
            availableModules.insert(candidateModule);
        }
        if (availableModules.size() > 1) {
            *error = QStringLiteral("class %1 is ambiguous across Qt modules; use Module.%1").arg(className);
            return nullptr;
        }
    }
    auto *widgetParent = qobject_cast<QWidget *>(parent);
    static const QSet<QString> widgetClasses = {
        QStringLiteral("QWidget"), QStringLiteral("QMainWindow"), QStringLiteral("QPushButton"),
        QStringLiteral("QToolButton"), QStringLiteral("QLabel"), QStringLiteral("QLineEdit"),
        QStringLiteral("QTextEdit"), QStringLiteral("QPlainTextEdit"), QStringLiteral("QCheckBox"),
        QStringLiteral("QRadioButton"), QStringLiteral("QComboBox"), QStringLiteral("QSpinBox"),
        QStringLiteral("QDoubleSpinBox"), QStringLiteral("QSlider"), QStringLiteral("QScrollBar"),
        QStringLiteral("QDial"), QStringLiteral("QProgressBar"), QStringLiteral("QGroupBox"),
        QStringLiteral("QTabWidget"), QStringLiteral("QTabBar"), QStringLiteral("QStackedWidget"),
        QStringLiteral("QVBoxLayout"), QStringLiteral("QHBoxLayout"), QStringLiteral("QFormLayout"),
        QStringLiteral("QGridLayout"), QStringLiteral("QStatusBar"), QStringLiteral("QDialog"),
        QStringLiteral("QDialogButtonBox"), QStringLiteral("QCalendarWidget"), QStringLiteral("QDateEdit"),
        QStringLiteral("QDateTimeEdit"), QStringLiteral("QTimeEdit"), QStringLiteral("QLCDNumber"),
        QStringLiteral("QScrollArea"), QStringLiteral("QSplitter"), QStringLiteral("QTableWidget"),
        QStringLiteral("QTreeWidget"), QStringLiteral("QListWidget"), QStringLiteral("QTableView"),
        QStringLiteral("QTreeView"), QStringLiteral("QListView"), QStringLiteral("QGraphicsView"),
        QStringLiteral("QMdiArea"), QStringLiteral("QToolBar"), QStringLiteral("QMenuBar"),
        QStringLiteral("QMenu"), QStringLiteral("QFrame"), QStringLiteral("QDockWidget"), QStringLiteral("QAxWidget"),
        QStringLiteral("QWizard"), QStringLiteral("QWizardPage"), QStringLiteral("QFontComboBox"),
        QStringLiteral("QKeySequenceEdit"), QStringLiteral("QMessageBox"), QStringLiteral("QFileDialog"),
        QStringLiteral("QColorDialog"), QStringLiteral("QFontDialog"), QStringLiteral("QInputDialog"),
        QStringLiteral("QProgressDialog"), QStringLiteral("QCommandLinkButton"), QStringLiteral("QTextBrowser"),
        QStringLiteral("QUndoView")
    };
    const bool isWidgetOrLayout = widgetClasses.contains(className);
    if (parent != nullptr && isWidgetOrLayout && widgetParent == nullptr) {
        *error = QStringLiteral("class %1 requires a QWidget parent").arg(className);
        return nullptr;
    }
    const bool noArgs = args.isEmpty();
    const bool optionalText = args.isEmpty() || (args.size() == 1 && args.first().isString());
    const QString text = optionalText && !args.isEmpty() ? args.first().toString() : QString();
    const bool createsWidgetLayout = className == QStringLiteral("QVBoxLayout") || className == QStringLiteral("QHBoxLayout") ||
                                     className == QStringLiteral("QFormLayout") || className == QStringLiteral("QGridLayout");
    if (createsWidgetLayout && widgetParent != nullptr && widgetParent->layout() != nullptr) {
        *error = QStringLiteral("QWidget already has a layout");
        return nullptr;
    }

    if (moduleName.isEmpty() || kry_qt6_generated::isExplicitConstructor(moduleName, className)) {
    if (className == QStringLiteral("QWidget") && noArgs) return new QWidget(widgetParent);
    if (className == QStringLiteral("QMainWindow") && args.isEmpty()) return new QMainWindow(widgetParent);
    if (className == QStringLiteral("QPushButton") && optionalText) return new QPushButton(text, widgetParent);
    if (className == QStringLiteral("QToolButton") && noArgs) return new QToolButton(widgetParent);
    if (className == QStringLiteral("QLabel") && optionalText) return new QLabel(text, widgetParent);
    if (className == QStringLiteral("QLineEdit") && optionalText) return new QLineEdit(text, widgetParent);
    if (className == QStringLiteral("QTextEdit") && optionalText) return new QTextEdit(text, widgetParent);
    if (className == QStringLiteral("QPlainTextEdit") && optionalText) return new QPlainTextEdit(text, widgetParent);
    if (className == QStringLiteral("QCheckBox") && optionalText) return new QCheckBox(text, widgetParent);
    if (className == QStringLiteral("QRadioButton") && optionalText) return new QRadioButton(text, widgetParent);
    if (className == QStringLiteral("QComboBox") && noArgs) return new QComboBox(widgetParent);
    if (className == QStringLiteral("QSpinBox") && noArgs) return new QSpinBox(widgetParent);
    if (className == QStringLiteral("QDoubleSpinBox") && noArgs) return new QDoubleSpinBox(widgetParent);
    if (className == QStringLiteral("QSlider") && noArgs) return new QSlider(Qt::Horizontal, widgetParent);
    if (className == QStringLiteral("QScrollBar") && noArgs) return new QScrollBar(Qt::Vertical, widgetParent);
    if (className == QStringLiteral("QDial") && noArgs) return new QDial(widgetParent);
    if (className == QStringLiteral("QProgressBar") && noArgs) return new QProgressBar(widgetParent);
    if (className == QStringLiteral("QGroupBox") && optionalText) return new QGroupBox(text, widgetParent);
    if (className == QStringLiteral("QTabWidget") && noArgs) return new QTabWidget(widgetParent);
    if (className == QStringLiteral("QTabBar") && noArgs) return new QTabBar(widgetParent);
    if (className == QStringLiteral("QStackedWidget") && noArgs) return new QStackedWidget(widgetParent);
    if (className == QStringLiteral("QVBoxLayout") && noArgs) return new QVBoxLayout(widgetParent);
    if (className == QStringLiteral("QHBoxLayout") && noArgs) return new QHBoxLayout(widgetParent);
    if (className == QStringLiteral("QFormLayout") && noArgs) return new QFormLayout(widgetParent);
    if (className == QStringLiteral("QGridLayout") && noArgs) return new QGridLayout(widgetParent);
    if (className == QStringLiteral("QStatusBar") && noArgs) return new QStatusBar(widgetParent);
    if (className == QStringLiteral("QDialog") && noArgs) return new QDialog(widgetParent);
    if (className == QStringLiteral("QDialogButtonBox") && noArgs) return new QDialogButtonBox(widgetParent);
    if (className == QStringLiteral("QCalendarWidget") && noArgs) return new QCalendarWidget(widgetParent);
    if (className == QStringLiteral("QDateEdit") && noArgs) return new QDateEdit(widgetParent);
    if (className == QStringLiteral("QDateTimeEdit") && noArgs) return new QDateTimeEdit(widgetParent);
    if (className == QStringLiteral("QTimeEdit") && noArgs) return new QTimeEdit(widgetParent);
    if (className == QStringLiteral("QLCDNumber") && noArgs) return new QLCDNumber(widgetParent);
    if (className == QStringLiteral("QScrollArea") && noArgs) return new QScrollArea(widgetParent);
    if (className == QStringLiteral("QSplitter") && noArgs) return new QSplitter(Qt::Horizontal, widgetParent);
    if (className == QStringLiteral("QTableWidget") && noArgs) return new QTableWidget(widgetParent);
    if (className == QStringLiteral("QTableWidget") && args.size() == 2 && args.at(0).isDouble() && args.at(1).isDouble()) {
        const double rows = args.at(0).toDouble(), columns = args.at(1).toDouble();
        if (std::isfinite(rows) && std::isfinite(columns) && std::floor(rows) == rows && std::floor(columns) == columns &&
            rows >= 0 && columns >= 0 && rows <= std::numeric_limits<int>::max() && columns <= std::numeric_limits<int>::max()) {
            return new QTableWidget(static_cast<int>(rows), static_cast<int>(columns), widgetParent);
        }
        *error = QStringLiteral("QTableWidget row and column counts must be non-negative 32-bit integers");
        return nullptr;
    }
    if (className == QStringLiteral("QTreeWidget") && noArgs) return new QTreeWidget(widgetParent);
    if (className == QStringLiteral("QListWidget") && noArgs) return new QListWidget(widgetParent);
    if (className == QStringLiteral("QTableView") && noArgs) return new QTableView(widgetParent);
    if (className == QStringLiteral("QTreeView") && noArgs) return new QTreeView(widgetParent);
    if (className == QStringLiteral("QListView") && noArgs) return new QListView(widgetParent);
    if (className == QStringLiteral("QGraphicsView") && noArgs) return new QGraphicsView(widgetParent);
    if (className == QStringLiteral("QMdiArea") && noArgs) return new QMdiArea(widgetParent);
    if (className == QStringLiteral("QToolBar") && optionalText) return new QToolBar(text, widgetParent);
    if (className == QStringLiteral("QMenuBar") && noArgs) return new QMenuBar(widgetParent);
    if (className == QStringLiteral("QMenu") && optionalText) return new QMenu(text, widgetParent);
    if (className == QStringLiteral("QFrame") && noArgs) return new QFrame(widgetParent);
    if (className == QStringLiteral("QDockWidget") && optionalText) return new QDockWidget(text, widgetParent);
    if (className == QStringLiteral("QWizard") && noArgs) return new QWizard(widgetParent);
    if (className == QStringLiteral("QWizardPage") && noArgs) return new QWizardPage(widgetParent);
    if (className == QStringLiteral("QFontComboBox") && noArgs) return new QFontComboBox(widgetParent);
    if (className == QStringLiteral("QKeySequenceEdit") && noArgs) return new QKeySequenceEdit(widgetParent);
    if (className == QStringLiteral("QMessageBox") && noArgs) return new QMessageBox(widgetParent);
    if (className == QStringLiteral("QFileDialog") && noArgs) return new QFileDialog(widgetParent);
    if (className == QStringLiteral("QColorDialog") && noArgs) return new QColorDialog(widgetParent);
    if (className == QStringLiteral("QFontDialog") && noArgs) return new QFontDialog(widgetParent);
    if (className == QStringLiteral("QInputDialog") && noArgs) return new QInputDialog(widgetParent);
    if (className == QStringLiteral("QProgressDialog") && noArgs) return new QProgressDialog(widgetParent);
#ifdef KRY_QT6_HAS_AXCONTAINER
    if (className == QStringLiteral("QAxObject") && optionalText) return new QAxObject(text, parent);
    if (className == QStringLiteral("QAxWidget") && optionalText) return new QAxWidget(text, widgetParent);
#endif
    if (className == QStringLiteral("QCommandLinkButton") && optionalText) return new QCommandLinkButton(text, QString(), widgetParent);
    if (className == QStringLiteral("QTimer") && noArgs) return new QTimer(parent);
    if (className == QStringLiteral("QAction") && optionalText) return new QAction(text, parent);
    if (className == QStringLiteral("QButtonGroup") && noArgs) return new QButtonGroup(parent);
    if (className == QStringLiteral("QStringListModel") && noArgs) return new QStringListModel(parent);
    if (className == QStringLiteral("QStandardItemModel") && noArgs) return new QStandardItemModel(parent);
    if (className == QStringLiteral("QSortFilterProxyModel") && noArgs) return new QSortFilterProxyModel(parent);
    if (className == QStringLiteral("QObject") && noArgs) return new QObject(parent);
    if (className == QStringLiteral("QBuffer") && noArgs) return new QBuffer(parent);
    if (className == QStringLiteral("QConcatenateTablesProxyModel") && noArgs) return new QConcatenateTablesProxyModel(parent);
    if (className == QStringLiteral("QEventLoop") && noArgs) return new QEventLoop(parent);
    if (className == QStringLiteral("QFile") && noArgs) return new QFile(parent);
    if (className == QStringLiteral("QFile") && optionalText) return new QFile(text, parent);
    if (className == QStringLiteral("QFileSystemWatcher") && noArgs) return new QFileSystemWatcher(parent);
    if (className == QStringLiteral("QIdentityProxyModel") && noArgs) return new QIdentityProxyModel(parent);
    if (className == QStringLiteral("QItemSelectionModel") && noArgs) return new QItemSelectionModel(nullptr, parent);
    if (className == QStringLiteral("QItemSelectionModel") && args.size() == 1) {
        auto *model = qobject_cast<QAbstractItemModel *>(objectArgument(args, 0, error));
        if (model == nullptr) {
            if (error->isEmpty()) *error = QStringLiteral("QItemSelectionModel expects a QAbstractItemModel handle");
            return nullptr;
        }
        return new QItemSelectionModel(model, parent);
    }
    if (className == QStringLiteral("QLibrary") && noArgs) return new QLibrary(parent);
    if (className == QStringLiteral("QLibrary") && optionalText) return new QLibrary(text, parent);
    if (className == QStringLiteral("QPluginLoader") && noArgs) return new QPluginLoader(parent);
    if (className == QStringLiteral("QPluginLoader") && optionalText) return new QPluginLoader(text, parent);
    if (className == QStringLiteral("QProcess") && noArgs) return new QProcess(parent);
    if (className == QStringLiteral("QSaveFile") && noArgs) return new QSaveFile(parent);
    if (className == QStringLiteral("QSaveFile") && optionalText) return new QSaveFile(text, parent);
    if (className == QStringLiteral("QSettings") && noArgs) return new QSettings(parent);
    if (className == QStringLiteral("QSettings") && optionalText) return new QSettings(text, QString(), parent);
    if (className == QStringLiteral("QSettings") && args.size() == 2 && args.at(0).isString() && args.at(1).isString()) {
        return new QSettings(args.at(0).toString(), args.at(1).toString(), parent);
    }
    if (className == QStringLiteral("QSignalMapper") && noArgs) return new QSignalMapper(parent);
    if (className == QStringLiteral("QTemporaryFile") && noArgs) return new QTemporaryFile(parent);
    if (className == QStringLiteral("QTemporaryFile") && optionalText) return new QTemporaryFile(text, parent);
    if (className == QStringLiteral("QThread") && noArgs) return new QThread(parent);
    if (className == QStringLiteral("QThreadPool") && noArgs) return new QThreadPool(parent);
    if (className == QStringLiteral("QTranslator") && noArgs) return new QTranslator(parent);
    if (className == QStringLiteral("QTransposeProxyModel") && noArgs) return new QTransposeProxyModel(parent);
    if (className == QStringLiteral("QMovie") && noArgs) return new QMovie(parent);
    if (className == QStringLiteral("QMovie") && optionalText) return new QMovie(text, QByteArray(), parent);
    if (className == QStringLiteral("QShortcut") && noArgs) return new QShortcut(parent);
    if (className == QStringLiteral("QTextDocument") && noArgs) return new QTextDocument(parent);
    if (className == QStringLiteral("QTextDocument") && optionalText) return new QTextDocument(text, parent);
    if (className == QStringLiteral("QUndoGroup") && noArgs) return new QUndoGroup(parent);
    if (className == QStringLiteral("QUndoStack") && noArgs) return new QUndoStack(parent);
    if (className == QStringLiteral("QGraphicsScene") && noArgs) return new QGraphicsScene(parent);
    if (className == QStringLiteral("QTextBrowser") && noArgs) return new QTextBrowser(widgetParent);
    if (className == QStringLiteral("QUndoView") && noArgs) return new QUndoView(widgetParent);
    }

    if (args.isEmpty()) {
        if (QObject *generated = kry_qt6_generated::construct(requestedClassName, parent, error); generated != nullptr) return generated;
        if (!error->isEmpty()) return nullptr;
    }
    QObject *generatedWithArguments = nullptr;
    if (kry_qt6_generated::constructArguments(moduleName, className, parent, args,
                                               &generatedWithArguments, error)) {
        if (generatedWithArguments != nullptr) return generatedWithArguments;
        if (!error->isEmpty()) return nullptr;
    }
    const QMetaObject *classMetaObject = kry_qt6_generated::classMetaObject(requestedClassName, error);
    if (!error->isEmpty()) return nullptr;
    if (classMetaObject != nullptr) {
        if (QObject *created = constructInvokable(classMetaObject, parent, args, error); created != nullptr) return created;
    }
    if (error->isEmpty()) *error = QStringLiteral("class %1 has no native constructor binding yet").arg(className);
    return nullptr;
}

bool invokeMethod(const QMetaObject *metaObject, QObject *object, void *gadget,
                  const QString &rawSignature, const QJsonArray &jsonArguments,
                  QJsonValue *result, QString *error) {
    if (hasNul(rawSignature)) {
        *error = QStringLiteral("method signature must not contain NUL characters");
        return false;
    }
    QByteArray signature = QMetaObject::normalizedSignature(rawSignature.toUtf8().constData());
    if (signature.isEmpty()) {
        *error = QStringLiteral("method must use a normalized signature, for example setText(QString)");
        return false;
    }
    const int index = metaObject->indexOfMethod(signature.constData());
    if (index < 0) {
        *error = QStringLiteral("method %1 is not exposed as a Qt slot or invokable method").arg(QString::fromLatin1(signature));
        return false;
    }
    const QMetaMethod method = metaObject->method(index);
    if (method.methodType() == QMetaMethod::Signal || method.access() != QMetaMethod::Public) {
        *error = QStringLiteral("only public non-signal methods can be invoked");
        return false;
    }
    QList<QMetaType> types;
    types.reserve(method.parameterCount());
    for (int i = 0; i < method.parameterCount(); ++i) {
        types.append(method.parameterMetaType(i));
    }
    QList<QVariant> values;
    std::array<QGenericArgument, kMaxInvocationArguments> arguments{};
    if (!buildArguments(jsonArguments, types, &values, &arguments, error)) {
        return false;
    }

    const QMetaType returnType = method.returnMetaType();
    QVariant returnValue;
    QGenericReturnArgument returnArgument;
    if (returnType.isValid() && returnType.id() != QMetaType::Void) {
        returnValue = QVariant(returnType);
        if (!returnValue.isValid()) {
            *error = QStringLiteral("Qt cannot construct return type %1").arg(QString::fromLatin1(returnType.name()));
            return false;
        }
        const QByteArray returnName(returnType.name());
        returnArgument = QGenericReturnArgument(returnName.constData(), returnValue.data());
    }
    const bool invoked = object != nullptr
                             ? method.invoke(object, Qt::DirectConnection, returnArgument,
                                             arguments[0], arguments[1], arguments[2], arguments[3], arguments[4],
                                             arguments[5], arguments[6], arguments[7], arguments[8], arguments[9])
                             : method.invokeOnGadget(gadget, returnArgument,
                                                     arguments[0], arguments[1], arguments[2], arguments[3], arguments[4],
                                                     arguments[5], arguments[6], arguments[7], arguments[8], arguments[9]);
    if (!invoked) {
        *error = QStringLiteral("Qt rejected the method arguments for %1").arg(QString::fromLatin1(signature));
        return false;
    }
    *result = returnType.isValid() && returnType.id() != QMetaType::Void ? toJson(returnValue) : QJsonValue(QJsonValue::Null);
    return true;
}

bool setProperty(QObject *object, const QString &name, const QJsonValue &jsonValue, QString *error) {
    if (name.isEmpty() || hasNul(name)) {
        *error = QStringLiteral("property name must be non-empty and contain no NUL character");
        return false;
    }
    const int index = object->metaObject()->indexOfProperty(name.toUtf8().constData());
    if (index < 0) {
        const QByteArray key = name.toUtf8();
        object->setProperty(key.constData(), jsonValue.toVariant());
        if (object->dynamicPropertyNames().contains(key)) return true;
        *error = QStringLiteral("unknown Qt property %1").arg(name);
        return false;
    }
    const QMetaProperty property = object->metaObject()->property(index);
    if (!property.isWritable()) {
        *error = QStringLiteral("Qt property %1 is read-only").arg(name);
        return false;
    }
    QVariant converted = fromJson(jsonValue, property.metaType(), error);
    if (!converted.isValid() && property.metaType().id() != QMetaType::Nullptr) {
        return false;
    }
    if (!property.write(object, converted)) {
        *error = QStringLiteral("Qt rejected a value for property %1").arg(name);
        return false;
    }
    return true;
}

QJsonObject metaObjectDescription(const QMetaObject *metaObject) {
    if (metaObject == nullptr) return {};
    QJsonArray properties;
    for (int i = 0; i < metaObject->propertyCount(); ++i) {
        const QMetaProperty property = metaObject->property(i);
        properties.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(property.name())},
                                       {QStringLiteral("type"), QString::fromLatin1(property.metaType().name())},
                                       {QStringLiteral("readable"), property.isReadable()},
                                       {QStringLiteral("writable"), property.isWritable()},
                                       {QStringLiteral("designable"), property.isDesignable()},
                                       {QStringLiteral("stored"), property.isStored()}});
    }
    QJsonArray methods;
    for (int i = 0; i < metaObject->methodCount(); ++i) {
        const QMetaMethod method = metaObject->method(i);
        QJsonArray parameters;
        const QList<QByteArray> parameterNames = method.parameterNames();
        const QList<QByteArray> parameterTypes = method.parameterTypes();
        for (qsizetype p = 0; p < parameterTypes.size(); ++p) {
            parameters.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(parameterNames.value(p))},
                                          {QStringLiteral("type"), QString::fromLatin1(parameterTypes.at(p))}});
        }
        QString kind;
        switch (method.methodType()) {
        case QMetaMethod::Signal: kind = QStringLiteral("signal"); break;
        case QMetaMethod::Slot: kind = QStringLiteral("slot"); break;
        case QMetaMethod::Constructor: kind = QStringLiteral("constructor"); break;
        case QMetaMethod::Method: kind = QStringLiteral("method"); break;
        }
        QString access;
        switch (method.access()) {
        case QMetaMethod::Private: access = QStringLiteral("private"); break;
        case QMetaMethod::Protected: access = QStringLiteral("protected"); break;
        case QMetaMethod::Public: access = QStringLiteral("public"); break;
        }
        methods.append(QJsonObject{{QStringLiteral("signature"), QString::fromLatin1(method.methodSignature())},
                                   {QStringLiteral("kind"), kind},
                                   {QStringLiteral("access"), access},
                                   {QStringLiteral("return_type"), QString::fromLatin1(method.typeName() == nullptr ? "void" : method.typeName())},
                                   {QStringLiteral("parameters"), parameters},
                                   {QStringLiteral("invokable"), method.isValid() && method.methodType() != QMetaMethod::Signal && method.methodType() != QMetaMethod::Constructor && method.access() == QMetaMethod::Public}});
    }
    QJsonArray enumerators;
    for (int i = 0; i < metaObject->enumeratorCount(); ++i) {
        const QMetaEnum enumeration = metaObject->enumerator(i);
        QJsonObject keys;
        for (int k = 0; k < enumeration.keyCount(); ++k) keys.insert(QString::fromLatin1(enumeration.key(k)), enumeration.value(k));
        enumerators.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(enumeration.name())},
                                       {QStringLiteral("scope"), QString::fromLatin1(enumeration.scope())},
                                       {QStringLiteral("is_flag"), enumeration.isFlag()},
                                       {QStringLiteral("keys"), keys}});
    }
    return QJsonObject{{QStringLiteral("class"), QString::fromLatin1(metaObject->className())},
                       {QStringLiteral("superclass"), metaObject->superClass() == nullptr ? QJsonValue(QJsonValue::Null) : QJsonValue(QString::fromLatin1(metaObject->superClass()->className()))},
                       {QStringLiteral("properties"), properties},
                       {QStringLiteral("methods"), methods},
                       {QStringLiteral("enumerators"), enumerators}};
}

QJsonObject handleRequest(const QJsonObject &request) {
    const QString operation = request.value(QStringLiteral("op")).toString();
    if (operation.isEmpty()) {
        return failure(QStringLiteral("request requires a string field named op"));
    }

    if (operation == QStringLiteral("constructors")) {
        QSet<QString> qualifiedNames;
        for (const QJsonValue &value : kry_qt6_generated::availableConstructors()) {
            const QString name = value.toString();
            if (name.contains(QLatin1Char('.'))) qualifiedNames.insert(name);
        }
        for (const QJsonValue &value : kry_qt6_generated::availableArgumentConstructors()) {
            const QString name = value.toString();
            if (name.contains(QLatin1Char('.'))) qualifiedNames.insert(name);
        }
        QMap<QString, int> counts;
        for (const QString &name : qualifiedNames) {
            const qsizetype separator = name.indexOf(QLatin1Char('.'));
            ++counts[name.mid(separator + 1)];
        }
        QStringList sorted = qualifiedNames.values();
        for (auto it = counts.cbegin(); it != counts.cend(); ++it) {
            if (it.value() == 1) sorted.append(it.key());
        }
        std::sort(sorted.begin(), sorted.end());
        QJsonArray constructors;
        for (const QString &name : sorted) constructors.append(name);
        return success(constructors);
    }
    if (operation == QStringLiteral("value_types")) {
        QSet<QString> types;
        for (const QJsonValue &value : kry_qt6_generated::availableValueTypes()) types.insert(value.toString());
        for (const QString &name : kry_qt6_direct::availableDirectValueTypes()) types.insert(name);
        for (const QString &name : kry_qt6_generated::availableDirectValueConstructors()) types.insert(name);
        QStringList sorted = types.values();
        std::sort(sorted.begin(), sorted.end());
        QJsonArray result;
        for (const QString &name : sorted) result.append(name);
        return success(result);
    }
    if (operation == QStringLiteral("meta_classes")) {
        return success(kry_qt6_generated::availableMetaObjects());
    }
    if (operation == QStringLiteral("describe_class")) {
        const QString className = request.value(QStringLiteral("class")).toString();
        if (className.isEmpty() || hasNul(className)) return failure(QStringLiteral("describe_class requires a non-empty Qt class name without NUL characters"));
        QString error;
        const QMetaObject *metaObject = kry_qt6_generated::classMetaObject(className, &error);
        if (!error.isEmpty()) return failure(error);
        if (metaObject == nullptr) return failure(QStringLiteral("Qt class %1 has no linked QObject meta-object").arg(className));
        return success(metaObjectDescription(metaObject));
    }
    if (operation == QStringLiteral("describe_value_type")) {
        const QString typeName = request.value(QStringLiteral("type")).toString();
        if (typeName.isEmpty() || hasNul(typeName)) return failure(QStringLiteral("describe_value_type requires a non-empty Qt value type name without NUL characters"));
        QString error;
        const QMetaType type = kry_qt6_generated::valueMetaType(typeName, &error);
        if (!error.isEmpty()) return failure(error);
        if (type.isValid() && type.metaObject() != nullptr) return success(metaObjectDescription(type.metaObject()));
        const QJsonValue direct = kry_qt6_direct::describeDirectValueType(typeName.section(QLatin1Char('.'), -1));
        if (!direct.isUndefined()) return success(direct);
        if (kry_qt6_generated::availableDirectValueConstructors().contains(typeName.section(QLatin1Char('.'), -1))) {
            return success(QJsonObject{{QStringLiteral("type"), typeName.section(QLatin1Char('.'), -1)},
                                       {QStringLiteral("kind"), QStringLiteral("value")},
                                       {QStringLiteral("methods"), QJsonArray()}});
        }
        return failure(QStringLiteral("Qt value type %1 has no generated meta-object or native method bindings").arg(typeName));
    }

    if (operation == QStringLiteral("application")) {
        if (application) {
            return success(QJsonObject{{QStringLiteral("running"), true}});
        }
        application = std::make_unique<QApplication>(applicationArgc, applicationArgv);
        applicationThread = QThread::currentThread();
        return success(QJsonObject{{QStringLiteral("running"), true}});
    }

    if (operation == QStringLiteral("shutdown")) {
        if (!application) return success();
        if (QThread::currentThread() != applicationThread) {
            return failure(QStringLiteral("Qt shutdown must run on the application thread"));
        }
        subscriptions.clear();
        std::vector<QPointer<QObject>> roots;
        for (const auto &entry : objects) {
            QObject *object = entry.second.data();
            if (object == nullptr) continue;
            QObject *parent = object->parent();
            if (parent == nullptr || idForObject(parent) == 0) roots.emplace_back(object);
        }
        for (const QPointer<QObject> &root : roots) {
            if (!root.isNull()) delete root.data();
        }
        for (const auto &entry : objects) {
            if (!entry.second.isNull()) delete entry.second.data();
        }
        objects.clear();
        values.clear();
        application.reset();
        applicationThread = nullptr;
        return success();
    }

    if (application == nullptr || QCoreApplication::instance() == nullptr) {
        return failure(QStringLiteral("start the Qt application before creating or using Qt objects"));
    }
    if (QThread::currentThread() != applicationThread) {
        return failure(QStringLiteral("Qt requests must run on the application thread"));
    }

    if (operation == QStringLiteral("value_new")) {
        const QString requestedType = request.value(QStringLiteral("type")).toString();
        if (requestedType.isEmpty() || hasNul(requestedType)) return failure(QStringLiteral("value_new requires a non-empty Qt value type name"));
        const qsizetype separator = requestedType.indexOf(QLatin1Char('.'));
        const QString moduleName = separator < 0 ? QString() : requestedType.left(separator);
        const QString className = separator < 0 ? requestedType : requestedType.mid(separator + 1);
        const QJsonValue argsValue = request.value(QStringLiteral("args"));
        if (!argsValue.isUndefined() && !argsValue.isArray()) return failure(QStringLiteral("value_new args must be a JSON array"));
        QString error;
        QVariant constructed;
        if (kry_qt6_generated::constructValueArguments(moduleName, className,
                                                       argsValue.toArray(), &constructed, &error)) {
            if (!constructed.isValid()) return failure(error.isEmpty()
                ? QStringLiteral("Qt value type %1 has no matching native constructor").arg(requestedType)
                : error);
            const qint64 id = registerValue(constructed);
            return success(QJsonObject{{QStringLiteral("value_id"), static_cast<double>(id)},
                                       {QStringLiteral("type"), QString::fromLatin1(constructed.metaType().name())}});
        }
        if (!error.isEmpty()) return failure(error);
        if (!argsValue.isUndefined() && !argsValue.toArray().isEmpty()) {
            return failure(QStringLiteral("Qt value type %1 has no generated constructor for %2 argument(s)")
                               .arg(requestedType).arg(argsValue.toArray().size()));
        }
        const QMetaType type = kry_qt6_generated::valueMetaType(requestedType, &error);
        if (!error.isEmpty()) return failure(error);
        if (!type.isValid()) return failure(QStringLiteral("Qt gadget type %1 has no generated default value constructor").arg(requestedType));
        QVariant value(type);
        if (!value.isValid()) return failure(QStringLiteral("Qt cannot default-construct value type %1").arg(requestedType));
        const qint64 id = registerValue(value);
        return success(QJsonObject{{QStringLiteral("value_id"), static_cast<double>(id)},
                                   {QStringLiteral("type"), QString::fromLatin1(type.name())}});
    }

    if (operation == QStringLiteral("new")) {
        const QString className = request.value(QStringLiteral("class")).toString();
        if (className.isEmpty() || hasNul(className)) {
            return failure(QStringLiteral("new requires a non-empty Qt class name without NUL characters"));
        }
        QObject *parent = nullptr;
        const QJsonValue parentValue = request.value(QStringLiteral("parent"));
        if (!parentValue.isUndefined() && !parentValue.isNull()) {
            qint64 parentId = 0;
            QString error;
            if (!parseObjectId(parentValue, &parentId, &error)) return failure(error);
            parent = objectForId(parentId);
            if (parent == nullptr) return failure(QStringLiteral("parent object handle is closed or unknown"));
        }
        const QJsonValue argsValue = request.value(QStringLiteral("args"));
        if (!argsValue.isUndefined() && !argsValue.isArray()) {
            return failure(QStringLiteral("new field args must be a JSON array"));
        }
        const QJsonArray args = argsValue.toArray();
        QString error;
        QObject *created = createObject(className, parent, args, &error);
        if (created == nullptr) return failure(error);
        if (parent != nullptr && created->parent() == nullptr) created->setParent(parent);
        const qint64 id = nextObjectId++;
        objects.emplace(id, QPointer<QObject>(created));
        return success(QJsonObject{{QStringLiteral("object_id"), static_cast<double>(id)},
                                   {QStringLiteral("class"), QString::fromLatin1(created->metaObject()->className())}});
    }

    if (operation == QStringLiteral("events")) {
        const QJsonValue waitValue = request.value(QStringLiteral("wait_ms"));
        const QJsonValue maximumValue = request.value(QStringLiteral("max"));
        if ((!waitValue.isUndefined() && !waitValue.isDouble()) ||
            (!maximumValue.isUndefined() && !maximumValue.isDouble())) {
            return failure(QStringLiteral("wait_ms and max must be integer JSON numbers"));
        }
        for (const QJsonValue &number : {waitValue, maximumValue}) {
            if (number.isUndefined()) continue;
            const double raw = number.toDouble();
            if (!std::isfinite(raw) || std::floor(raw) != raw || raw < 0 || raw > 2147483647.0) {
                return failure(QStringLiteral("wait_ms and max must be non-negative 32-bit integers"));
            }
        }
        const int waitMilliseconds = request.value(QStringLiteral("wait_ms")).toInt(0);
        const int maximum = request.value(QStringLiteral("max")).toInt(64);
        if (waitMilliseconds < 0 || waitMilliseconds > kMaxWaitMilliseconds) {
            return failure(QStringLiteral("wait_ms must be in 0..30000"));
        }
        if (maximum < 1 || maximum > kMaxEventsPerPoll) {
            return failure(QStringLiteral("max must be in 1..1000"));
        }
        if (waitMilliseconds > 0) {
            QEventLoop loop;
            QTimer::singleShot(waitMilliseconds, &loop, &QEventLoop::quit);
            loop.exec();
        } else {
            QCoreApplication::processEvents(QEventLoop::AllEvents);
        }
        QJsonArray events;
        for (auto group = subscriptions.begin(); group != subscriptions.end();) {
            if (objectForId(group->first) == nullptr) {
                group = subscriptions.erase(group);
                continue;
            }
            for (auto &subscription : group->second) {
                if (!subscription->spy || !subscription->spy->isValid()) continue;
                while (!subscription->spy->isEmpty() && events.size() < maximum) {
                    const QList<QVariant> arguments = subscription->spy->takeFirst();
                    QJsonArray jsonArguments;
                    for (const QVariant &argument : arguments) jsonArguments.append(toJson(argument));
                    events.append(QJsonObject{{QStringLiteral("object_id"), static_cast<double>(subscription->objectId)},
                                              {QStringLiteral("signal"), QString::fromLatin1(subscription->signature)},
                                              {QStringLiteral("args"), jsonArguments}});
                }
                if (events.size() >= maximum) break;
            }
            if (events.size() >= maximum) break;
            ++group;
        }
        return success(events);
    }

    if (operation == QStringLiteral("describe")) {
        qint64 id = 0;
        QString error;
        if (!parseObjectId(request.value(QStringLiteral("object_id")), &id, &error)) return failure(error);
        QObject *object = objectForId(id);
        if (object == nullptr) return failure(QStringLiteral("Qt object handle is closed or unknown"));
        const QMetaObject *metaObject = object->metaObject();
        QJsonArray properties;
        for (int i = 0; i < metaObject->propertyCount(); ++i) {
            const QMetaProperty property = metaObject->property(i);
            properties.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(property.name())},
                                           {QStringLiteral("type"), QString::fromLatin1(property.metaType().name())},
                                           {QStringLiteral("readable"), property.isReadable()},
                                           {QStringLiteral("writable"), property.isWritable()},
                                           {QStringLiteral("designable"), property.isDesignable()},
                                           {QStringLiteral("stored"), property.isStored()}});
        }
        QJsonArray methods;
        for (int i = 0; i < metaObject->methodCount(); ++i) {
            const QMetaMethod method = metaObject->method(i);
            QJsonArray parameters;
            const QList<QByteArray> parameterNames = method.parameterNames();
            const QList<QByteArray> parameterTypes = method.parameterTypes();
            for (qsizetype p = 0; p < parameterTypes.size(); ++p) {
                parameters.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(parameterNames.value(p))},
                                              {QStringLiteral("type"), QString::fromLatin1(parameterTypes.at(p))}});
            }
            QString kind;
            switch (method.methodType()) {
            case QMetaMethod::Signal: kind = QStringLiteral("signal"); break;
            case QMetaMethod::Slot: kind = QStringLiteral("slot"); break;
            case QMetaMethod::Constructor: kind = QStringLiteral("constructor"); break;
            case QMetaMethod::Method: kind = QStringLiteral("method"); break;
            }
            QString access;
            switch (method.access()) {
            case QMetaMethod::Private: access = QStringLiteral("private"); break;
            case QMetaMethod::Protected: access = QStringLiteral("protected"); break;
            case QMetaMethod::Public: access = QStringLiteral("public"); break;
            }
            methods.append(QJsonObject{{QStringLiteral("signature"), QString::fromLatin1(method.methodSignature())},
                                       {QStringLiteral("kind"), kind},
                                       {QStringLiteral("access"), access},
                                       {QStringLiteral("return_type"), QString::fromLatin1(method.typeName() == nullptr ? "void" : method.typeName())},
                                       {QStringLiteral("parameters"), parameters},
                                       {QStringLiteral("invokable"), method.isValid() && method.methodType() != QMetaMethod::Signal && method.methodType() != QMetaMethod::Constructor && method.access() == QMetaMethod::Public}});
        }
        QJsonArray enumerators;
        for (int i = 0; i < metaObject->enumeratorCount(); ++i) {
            const QMetaEnum enumeration = metaObject->enumerator(i);
            QJsonObject keys;
            for (int k = 0; k < enumeration.keyCount(); ++k) {
                keys.insert(QString::fromLatin1(enumeration.key(k)), enumeration.value(k));
            }
            enumerators.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(enumeration.name())},
                                           {QStringLiteral("scope"), QString::fromLatin1(enumeration.scope())},
                                           {QStringLiteral("is_flag"), enumeration.isFlag()},
                                           {QStringLiteral("keys"), keys}});
        }
        return success(QJsonObject{{QStringLiteral("class"), QString::fromLatin1(metaObject->className())},
                                   {QStringLiteral("superclass"), metaObject->superClass() == nullptr ? QJsonValue(QJsonValue::Null) : QJsonValue(QString::fromLatin1(metaObject->superClass()->className()))},
                                   {QStringLiteral("properties"), properties},
                                   {QStringLiteral("methods"), methods},
                                   {QStringLiteral("enumerators"), enumerators}});
    }

    if (operation.startsWith(QStringLiteral("value_"))) {
        const QJsonValue rawId = request.value(QStringLiteral("value_id"));
        if (!rawId.isDouble() || !std::isfinite(rawId.toDouble()) || std::floor(rawId.toDouble()) != rawId.toDouble() ||
            rawId.toDouble() < 1 || rawId.toDouble() > 9007199254740991.0) {
            return failure(QStringLiteral("value operation requires a positive integer value_id"));
        }
        const qint64 valueId = static_cast<qint64>(rawId.toDouble());
        auto value = values.find(valueId);
        if (value == values.end()) return failure(QStringLiteral("Qt value handle is closed or unknown"));
        QString error;
        if (operation == QStringLiteral("value_delete")) {
            values.erase(value);
            return success();
        }
        const QMetaObject *metaObject = value->second.metaType().metaObject();
        if (operation == QStringLiteral("value_call")) {
            const QString signature = request.value(QStringLiteral("method")).toString();
            const QJsonValue argsValue = request.value(QStringLiteral("args"));
            if (!argsValue.isUndefined() && !argsValue.isArray()) return failure(QStringLiteral("value_call args must be a JSON array"));
            const QString className = QString::fromLatin1(value->second.metaType().name());
            QJsonValue result;
            QString reflectionError;
            if (metaObject != nullptr &&
                invokeMethod(metaObject, nullptr, value->second.data(), signature,
                             argsValue.toArray(), &result, &reflectionError)) return success(result);
            QString directError;
            if (kry_qt6_direct::invokeValue(&value->second, className, signature,
                                             argsValue.toArray(), &result, &directError)) return success(result);
            if (!directError.isEmpty()) return failure(directError);
            if (!reflectionError.isEmpty()) return failure(reflectionError);
            return failure(QStringLiteral("Qt value type %1 has no generated method %2")
                               .arg(className, signature));
        }
        if (operation == QStringLiteral("value_describe") && metaObject == nullptr) {
            const QString className = QString::fromLatin1(value->second.metaType().name());
            const QJsonValue direct = kry_qt6_direct::describeDirectValueType(className);
            if (!direct.isUndefined()) return success(direct);
            return success(QJsonObject{{QStringLiteral("type"), className},
                                       {QStringLiteral("kind"), QStringLiteral("value")},
                                       {QStringLiteral("methods"), QJsonArray()}});
        }
        if (metaObject == nullptr) return failure(QStringLiteral("Qt value type has no gadget property meta-object"));
        if (operation == QStringLiteral("value_get")) {
            const QString name = request.value(QStringLiteral("name")).toString();
            const int index = metaObject->indexOfProperty(name.toUtf8().constData());
            if (index < 0) return failure(QStringLiteral("unknown Qt gadget property %1").arg(name));
            const QMetaProperty property = metaObject->property(index);
            if (!property.isReadable()) return failure(QStringLiteral("Qt gadget property %1 is not readable").arg(name));
            return success(toJson(property.readOnGadget(value->second.constData())));
        }
        if (operation == QStringLiteral("value_set")) {
            const QString name = request.value(QStringLiteral("name")).toString();
            const int index = metaObject->indexOfProperty(name.toUtf8().constData());
            if (index < 0) return failure(QStringLiteral("unknown Qt gadget property %1").arg(name));
            const QMetaProperty property = metaObject->property(index);
            if (!property.isWritable()) return failure(QStringLiteral("Qt gadget property %1 is read-only").arg(name));
            const QVariant converted = fromJson(request.value(QStringLiteral("value")), property.metaType(), &error);
            if (!converted.isValid()) return failure(error);
            if (!property.writeOnGadget(value->second.data(), converted)) return failure(QStringLiteral("Qt rejected a value for gadget property %1").arg(name));
            return success();
        }
        if (operation == QStringLiteral("value_describe")) {
            QJsonArray properties;
            for (int i = 0; i < metaObject->propertyCount(); ++i) {
                const QMetaProperty property = metaObject->property(i);
                properties.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(property.name())},
                                               {QStringLiteral("type"), QString::fromLatin1(property.metaType().name())},
                                               {QStringLiteral("readable"), property.isReadable()},
                                               {QStringLiteral("writable"), property.isWritable()}});
            }
            QJsonArray methods;
            for (int i = 0; i < metaObject->methodCount(); ++i) {
                const QMetaMethod method = metaObject->method(i);
                methods.append(QJsonObject{{QStringLiteral("signature"), QString::fromLatin1(method.methodSignature())},
                                           {QStringLiteral("kind"), method.methodType() == QMetaMethod::Signal ? QStringLiteral("signal") : QStringLiteral("method")},
                                           {QStringLiteral("public"), method.access() == QMetaMethod::Public}});
            }
            QJsonArray enums;
            for (int i = 0; i < metaObject->enumeratorCount(); ++i) {
                const QMetaEnum enumeration = metaObject->enumerator(i);
                QJsonObject keys;
                for (int k = 0; k < enumeration.keyCount(); ++k) keys.insert(QString::fromLatin1(enumeration.key(k)), enumeration.value(k));
                enums.append(QJsonObject{{QStringLiteral("name"), QString::fromLatin1(enumeration.name())},
                                          {QStringLiteral("is_flag"), enumeration.isFlag()},
                                          {QStringLiteral("keys"), keys}});
            }
            return success(QJsonObject{{QStringLiteral("class"), QString::fromLatin1(metaObject->className())},
                                       {QStringLiteral("properties"), properties},
                                       {QStringLiteral("methods"), methods},
                                       {QStringLiteral("enumerators"), enums}});
        }
        return failure(QStringLiteral("unknown Qt value operation %1").arg(operation));
    }

    if (operation == QStringLiteral("call_static")) {
        const QString className = request.value(QStringLiteral("class")).toString();
        const QString signature = request.value(QStringLiteral("method")).toString();
        const QJsonValue argsValue = request.value(QStringLiteral("args"));
        if (!argsValue.isArray()) return failure(QStringLiteral("call_static args must be a JSON array"));
        const QJsonArray args = argsValue.toArray();
        if (className.isEmpty() || hasNul(className) || signature.isEmpty() || hasNul(signature)) {
            return failure(QStringLiteral("call_static requires class and method names without NUL characters"));
        }
        QJsonValue directResult;
        QString directError;
        if (!kry_qt6_direct::invoke(nullptr, className, signature, args, &directResult, &directError)) {
            return failure(QStringLiteral("static method %1 is not exposed for %2").arg(signature, className));
        }
        if (!directError.isEmpty()) return failure(directError);
        return success(directResult);
    }

    qint64 id = 0;
    QString error;
    if (!parseObjectId(request.value(QStringLiteral("object_id")), &id, &error)) return failure(error);
    QObject *object = objectForId(id);
    if (object == nullptr) return failure(QStringLiteral("Qt object handle is closed or unknown"));

    if (operation == QStringLiteral("get")) {
        const QString name = request.value(QStringLiteral("name")).toString();
        if (name.isEmpty() || hasNul(name)) return failure(QStringLiteral("get requires a non-empty property name without NUL characters"));
        const int index = object->metaObject()->indexOfProperty(name.toUtf8().constData());
        if (index >= 0) return success(toJson(object->metaObject()->property(index).read(object)));
        const QByteArray key = name.toUtf8();
        if (object->dynamicPropertyNames().contains(key)) return success(toJson(object->property(key.constData())));
        return failure(QStringLiteral("unknown Qt property %1").arg(name));
    }

    if (operation == QStringLiteral("set")) {
        if (!setProperty(object, request.value(QStringLiteral("name")).toString(), request.value(QStringLiteral("value")), &error)) {
            return failure(error);
        }
        return success();
    }

    if (operation == QStringLiteral("call")) {
        const QString signature = request.value(QStringLiteral("method")).toString();
        if (signature.isEmpty()) return failure(QStringLiteral("call requires a method signature"));
        const QJsonValue argsValue = request.value(QStringLiteral("args"));
        if (!argsValue.isUndefined() && !argsValue.isArray()) {
            return failure(QStringLiteral("call field args must be a JSON array"));
        }
        const QJsonArray args = argsValue.toArray();
        auto integerArgument = [&](qsizetype index, int *value) {
            return index >= 0 && index < args.size() && readJsonInteger(args.at(index), value);
        };
        auto integer64Argument = [&](qsizetype index, qint64 *value) {
            if (index < 0 || index >= args.size() || !args.at(index).isDouble()) return false;
            const double raw = args.at(index).toDouble();
            if (!std::isfinite(raw) || std::floor(raw) != raw || std::abs(raw) > 9007199254740991.0) return false;
            *value = static_cast<qint64>(raw);
            return true;
        };
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr &&
            (signature == QStringLiteral("move(int,int)") || signature == QStringLiteral("resize(int,int)"))) {
            int first = 0, second = 0;
            if (args.size() != 2 || !integerArgument(0, &first) || !integerArgument(1, &second))
                return failure(QStringLiteral("QWidget move/resize expects two 32-bit integers"));
            if (signature == QStringLiteral("move(int,int)")) widget->move(first, second);
            else widget->resize(first, second);
            return success();
        }
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr &&
            (signature == QStringLiteral("move(QPoint)") || signature == QStringLiteral("resize(QSize)"))) {
            if (args.size() != 1) return failure(QStringLiteral("QWidget move/resize expects one Qt geometry value"));
            if (signature == QStringLiteral("move(QPoint)")) {
                const QVariant value = fromJson(args.first(), QMetaType::fromType<QPoint>(), &error);
                if (!value.isValid()) return failure(error);
                widget->move(value.value<QPoint>());
            } else {
                const QVariant value = fromJson(args.first(), QMetaType::fromType<QSize>(), &error);
                if (!value.isValid()) return failure(error);
                widget->resize(value.value<QSize>());
            }
            return success();
        }
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr &&
            (signature == QStringLiteral("setGeometry(int,int,int,int)"))) {
            int x = 0, y = 0, width = 0, height = 0;
            if (args.size() != 4 || !integerArgument(0, &x) || !integerArgument(1, &y) ||
                !integerArgument(2, &width) || !integerArgument(3, &height))
                return failure(QStringLiteral("QWidget setGeometry expects four 32-bit integers"));
            widget->setGeometry(x, y, width, height);
            return success();
        }
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr &&
            (signature == QStringLiteral("setGeometry(QRect)") || signature == QStringLiteral("setMinimumSize(QSize)") ||
             signature == QStringLiteral("setMaximumSize(QSize)") || signature == QStringLiteral("setFixedSize(QSize)"))) {
            if (args.size() != 1) return failure(QStringLiteral("QWidget geometry setter expects one Qt geometry value"));
            if (signature == QStringLiteral("setGeometry(QRect)")) {
                const QVariant value = fromJson(args.first(), QMetaType::fromType<QRect>(), &error);
                if (!value.isValid()) return failure(error);
                widget->setGeometry(value.value<QRect>());
            } else {
                const QVariant value = fromJson(args.first(), QMetaType::fromType<QSize>(), &error);
                if (!value.isValid()) return failure(error);
                if (signature == QStringLiteral("setMinimumSize(QSize)")) widget->setMinimumSize(value.value<QSize>());
                else if (signature == QStringLiteral("setMaximumSize(QSize)")) widget->setMaximumSize(value.value<QSize>());
                else widget->setFixedSize(value.value<QSize>());
            }
            return success();
        }
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr &&
            (signature == QStringLiteral("setMinimumSize(int,int)") || signature == QStringLiteral("setMaximumSize(int,int)") ||
             signature == QStringLiteral("setFixedSize(int,int)"))) {
            int width = 0, height = 0;
            if (args.size() != 2 || !integerArgument(0, &width) || !integerArgument(1, &height))
                return failure(QStringLiteral("QWidget size setter expects two 32-bit integers"));
            if (signature == QStringLiteral("setMinimumSize(int,int)")) widget->setMinimumSize(width, height);
            else if (signature == QStringLiteral("setMaximumSize(int,int)")) widget->setMaximumSize(width, height);
            else widget->setFixedSize(width, height);
            return success();
        }
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr &&
            (signature == QStringLiteral("width()") || signature == QStringLiteral("height()") ||
             signature == QStringLiteral("size()") || signature == QStringLiteral("pos()") ||
             signature == QStringLiteral("geometry()") || signature == QStringLiteral("frameGeometry()") ||
             signature == QStringLiteral("frameSize()") || signature == QStringLiteral("minimumSize()") ||
             signature == QStringLiteral("maximumSize()") || signature == QStringLiteral("contentsRect()"))) {
            if (!args.isEmpty()) return failure(QStringLiteral("QWidget geometry getter takes no arguments"));
            if (signature == QStringLiteral("width()")) return success(QJsonValue(widget->width()));
            if (signature == QStringLiteral("height()")) return success(QJsonValue(widget->height()));
            if (signature == QStringLiteral("size()")) return success(toJson(QVariant::fromValue(widget->size())));
            if (signature == QStringLiteral("pos()")) return success(toJson(QVariant::fromValue(widget->pos())));
            if (signature == QStringLiteral("geometry()")) return success(toJson(QVariant::fromValue(widget->geometry())));
            if (signature == QStringLiteral("frameGeometry()")) return success(toJson(QVariant::fromValue(widget->frameGeometry())));
            if (signature == QStringLiteral("frameSize()")) return success(toJson(QVariant::fromValue(widget->frameSize())));
            if (signature == QStringLiteral("minimumSize()")) return success(toJson(QVariant::fromValue(widget->minimumSize())));
            if (signature == QStringLiteral("maximumSize()")) return success(toJson(QVariant::fromValue(widget->maximumSize())));
            return success(toJson(QVariant::fromValue(widget->contentsRect())));
        }
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr && signature == QStringLiteral("window()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QWidget window() takes no arguments"));
            return success(objectHandleValue(widget->window()));
        }
#ifdef KRY_QT6_HAS_AXCONTAINER
        QAxBase *axBase = nullptr;
        if (auto *axObject = qobject_cast<QAxObject *>(object); axObject != nullptr) axBase = static_cast<QAxBase *>(axObject);
        else if (auto *axWidget = qobject_cast<QAxWidget *>(object); axWidget != nullptr) axBase = static_cast<QAxBase *>(axWidget);
        if (axBase != nullptr) {
            if (signature == QStringLiteral("control()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("control() takes no arguments"));
                return success(QJsonValue(axBase->control()));
            }
            if (signature == QStringLiteral("isNull()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("isNull() takes no arguments"));
                return success(QJsonValue(axBase->isNull()));
            }
            if (signature == QStringLiteral("setControl(QString)")) {
                if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("setControl(QString) requires one string"));
                return success(QJsonValue(axBase->setControl(args.first().toString())));
            }
            if (signature == QStringLiteral("clear()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("clear() takes no arguments"));
                axBase->clear();
                return success();
            }
            if (signature == QStringLiteral("classContext()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("classContext() takes no arguments"));
                return success(QJsonValue(static_cast<double>(axBase->classContext())));
            }
            if (signature == QStringLiteral("setClassContext(int)")) {
                int context = 0;
                if (args.size() != 1 || !integerArgument(0, &context) || context < 0) return failure(QStringLiteral("setClassContext(int) requires a non-negative 32-bit context value"));
                axBase->setClassContext(static_cast<ulong>(context));
                return success();
            }
            if (signature == QStringLiteral("verbs()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("verbs() takes no arguments"));
                return success(QJsonArray::fromStringList(axBase->verbs()));
            }
            if (signature == QStringLiteral("propertyBag()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("propertyBag() takes no arguments"));
                return success(toJson(QVariant::fromValue(axBase->propertyBag())));
            }
            if (signature == QStringLiteral("setPropertyBag(QVariantMap)")) {
                if (args.size() != 1 || !args.first().isObject()) return failure(QStringLiteral("setPropertyBag(QVariantMap) requires one object"));
                QAxBase::PropertyBag bag;
                const QJsonObject entries = args.first().toObject();
                for (auto it = entries.constBegin(); it != entries.constEnd(); ++it) bag.insert(it.key(), it.value().toVariant());
                axBase->setPropertyBag(bag);
                return success();
            }
            if (signature == QStringLiteral("dynamicCall(QString)")) {
                if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("dynamicCall(QString) requires one method expression"));
                const QByteArray method = args.first().toString().toUtf8();
                return success(toJson(axBase->dynamicCall(method.constData())));
            }
            if (signature == QStringLiteral("dynamicCall(QString,QVariantList)")) {
                if (args.size() != 2 || !args.first().isString() || !args.at(1).isArray()) return failure(QStringLiteral("dynamicCall(QString,QVariantList) requires a method expression and an argument array"));
                const QByteArray method = args.first().toString().toUtf8();
                QList<QVariant> values;
                for (const QJsonValue &value : args.at(1).toArray()) values.append(value.toVariant());
                return success(toJson(axBase->dynamicCall(method.constData(), values)));
            }
            if (signature == QStringLiteral("querySubObject(QString)")) {
                if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("querySubObject(QString) requires one method expression"));
                const QByteArray method = args.first().toString().toUtf8();
                QAxObject *child = axBase->querySubObject(method.constData());
                if (child == nullptr) return failure(QStringLiteral("ActiveX querySubObject returned no object"));
                return success(objectHandleValue(child));
            }
            if (signature == QStringLiteral("querySubObject(QString,QVariantList)")) {
                if (args.size() != 2 || !args.first().isString() || !args.at(1).isArray()) return failure(QStringLiteral("querySubObject(QString,QVariantList) requires a method expression and an argument array"));
                const QByteArray method = args.first().toString().toUtf8();
                QList<QVariant> values;
                for (const QJsonValue &value : args.at(1).toArray()) values.append(value.toVariant());
                QAxObject *child = axBase->querySubObject(method.constData(), values);
                if (child == nullptr) return failure(QStringLiteral("ActiveX querySubObject returned no object"));
                return success(objectHandleValue(child));
            }
            if (signature == QStringLiteral("propertyWritable(QString)")) {
                if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("propertyWritable(QString) requires one property name"));
                const QByteArray name = args.first().toString().toUtf8();
                return success(QJsonValue(axBase->propertyWritable(name.constData())));
            }
            if (signature == QStringLiteral("setPropertyWritable(QString,bool)")) {
                if (args.size() != 2 || !args.first().isString() || !args.at(1).isBool()) return failure(QStringLiteral("setPropertyWritable(QString,bool) requires a property name and boolean"));
                const QByteArray name = args.first().toString().toUtf8();
                axBase->setPropertyWritable(name.constData(), args.at(1).toBool());
                return success();
            }
            if (signature == QStringLiteral("asVariant()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("asVariant() takes no arguments"));
                return success(toJson(axBase->asVariant()));
            }
            if (signature == QStringLiteral("generateDocumentation()")) {
                if (!args.isEmpty()) return failure(QStringLiteral("generateDocumentation() takes no arguments"));
                return success(QJsonValue(axBase->generateDocumentation()));
            }
        }
        if (auto *axObject = qobject_cast<QAxObject *>(object); axObject != nullptr && signature == QStringLiteral("doVerb(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("doVerb(QString) requires one verb name"));
            return success(QJsonValue(axObject->doVerb(args.first().toString())));
        }
        if (auto *axObject = qobject_cast<QAxObject *>(object); axObject != nullptr && signature == QStringLiteral("resetControl()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("resetControl() takes no arguments"));
            axObject->resetControl();
            return success();
        }
        if (auto *axWidget = qobject_cast<QAxWidget *>(object); axWidget != nullptr && signature == QStringLiteral("doVerb(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("doVerb(QString) requires one verb name"));
            return success(QJsonValue(axWidget->doVerb(args.first().toString())));
        }
        if (auto *axWidget = qobject_cast<QAxWidget *>(object); axWidget != nullptr && signature == QStringLiteral("resetControl()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("resetControl() takes no arguments"));
            axWidget->resetControl();
            return success();
        }
#endif
        if (auto *manager = qobject_cast<QNetworkAccessManager *>(object); manager != nullptr &&
            (signature == QStringLiteral("get(QNetworkRequest)") || signature == QStringLiteral("head(QNetworkRequest)") ||
             signature == QStringLiteral("deleteResource(QNetworkRequest)"))) {
            if (args.size() != 1) return failure(QStringLiteral("QNetworkAccessManager request methods expect one QNetworkRequest value"));
            const QVariant requestValue = fromJson(args.first(), QMetaType::fromType<QNetworkRequest>(), &error);
            if (!requestValue.isValid()) return failure(error);
            const QNetworkRequest request = requestValue.value<QNetworkRequest>();
            QNetworkReply *reply = nullptr;
            if (signature == QStringLiteral("get(QNetworkRequest)")) reply = manager->get(request);
            else if (signature == QStringLiteral("head(QNetworkRequest)")) reply = manager->head(request);
            else reply = manager->deleteResource(request);
            return success(objectHandleValue(reply));
        }
        if (auto *manager = qobject_cast<QNetworkAccessManager *>(object); manager != nullptr &&
            (signature == QStringLiteral("post(QNetworkRequest,QByteArray)") || signature == QStringLiteral("put(QNetworkRequest,QByteArray)"))) {
            if (args.size() != 2) return failure(QStringLiteral("QNetworkAccessManager post/put expect a request and QByteArray value"));
            const QVariant requestValue = fromJson(args.at(0), QMetaType::fromType<QNetworkRequest>(), &error);
            if (!requestValue.isValid()) return failure(error);
            const QVariant dataValue = fromJson(args.at(1), QMetaType::fromType<QByteArray>(), &error);
            if (!dataValue.isValid()) return failure(error);
            const QNetworkRequest request = requestValue.value<QNetworkRequest>();
            const QByteArray data = dataValue.toByteArray();
            QNetworkReply *reply = signature == QStringLiteral("post(QNetworkRequest,QByteArray)")
                                       ? manager->post(request, data) : manager->put(request, data);
            return success(objectHandleValue(reply));
        }
        if (auto *reply = qobject_cast<QNetworkReply *>(object); reply != nullptr && signature == QStringLiteral("errorString()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QNetworkReply errorString() takes no arguments"));
            return success(QJsonValue(reply->errorString()));
        }
        if (auto *reply = qobject_cast<QNetworkReply *>(object); reply != nullptr && signature == QStringLiteral("attribute(QNetworkRequest::Attribute)")) {
            int attribute = 0;
            if (args.size() != 1 || !integerArgument(0, &attribute)) return failure(QStringLiteral("QNetworkReply attribute expects one integer QNetworkRequest::Attribute"));
            return success(toJson(reply->attribute(static_cast<QNetworkRequest::Attribute>(attribute))));
        }
        if (auto *server = qobject_cast<QTcpServer *>(object); server != nullptr &&
            (signature == QStringLiteral("listen(QHostAddress,quint16)") || signature == QStringLiteral("listen(QHostAddress,ushort)"))) {
            int port = 0;
            if (args.size() != 2 || !args.first().isString() || !integerArgument(1, &port) || port < 0 || port > 65535) {
                return failure(QStringLiteral("QTcpServer listen expects an IP address string and port in 0..65535"));
            }
            QHostAddress address;
            if (!address.setAddress(args.first().toString())) return failure(QStringLiteral("invalid QTcpServer IP address"));
            if (!server->listen(address, static_cast<quint16>(port))) return failure(server->errorString());
            return success(QJsonValue(true));
        }
        if (auto *server = qobject_cast<QTcpServer *>(object); server != nullptr && signature == QStringLiteral("serverPort()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QTcpServer serverPort() takes no arguments"));
            return success(QJsonValue(server->serverPort()));
        }
        if (auto *server = qobject_cast<QTcpServer *>(object); server != nullptr && signature == QStringLiteral("close()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QTcpServer close() takes no arguments"));
            server->close();
            return success();
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("open(QIODevice::OpenMode)")) {
            int mode = 0;
            if (args.size() != 1 || !integerArgument(0, &mode)) return failure(QStringLiteral("QIODevice open expects an integer OpenMode bitmask"));
            const bool opened = device->open(QIODevice::OpenMode(static_cast<QIODevice::OpenModeFlag>(mode)));
            if (!opened) return failure(device->errorString());
            return success(QJsonValue(true));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("close()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice close() takes no arguments"));
            device->close();
            return success();
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("isOpen()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice isOpen() takes no arguments"));
            return success(QJsonValue(device->isOpen()));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("isReadable()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice isReadable() takes no arguments"));
            return success(QJsonValue(device->isReadable()));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("isWritable()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice isWritable() takes no arguments"));
            return success(QJsonValue(device->isWritable()));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("atEnd()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice atEnd() takes no arguments"));
            return success(QJsonValue(device->atEnd()));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("readAll()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice readAll() takes no arguments"));
            return success(toJson(QVariant::fromValue(device->readAll())));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("readLine()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice readLine() takes no arguments"));
            return success(toJson(QVariant::fromValue(device->readLine())));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("write(QByteArray)")) {
            if (args.size() != 1) return failure(QStringLiteral("QIODevice write expects one QByteArray value"));
            const QVariant data = fromJson(args.first(), QMetaType::fromType<QByteArray>(), &error);
            if (!data.isValid()) return failure(error);
            const QByteArray bytes = data.toByteArray();
            const qint64 written = device->write(bytes);
            if (written < 0) return failure(device->errorString());
            return success(QJsonValue(static_cast<double>(written)));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("pos()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice pos() takes no arguments"));
            return success(QJsonValue(static_cast<double>(device->pos())));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("size()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QIODevice size() takes no arguments"));
            return success(QJsonValue(static_cast<double>(device->size())));
        }
        if (auto *device = qobject_cast<QIODevice *>(object); device != nullptr && signature == QStringLiteral("seek(qint64)")) {
            qint64 position = 0;
            if (args.size() != 1 || !integer64Argument(0, &position)) return failure(QStringLiteral("QIODevice seek expects one safe-range integer position"));
            if (!device->seek(position)) return failure(device->errorString());
            return success(QJsonValue(true));
        }
        if (auto *settings = qobject_cast<QSettings *>(object); settings != nullptr && signature == QStringLiteral("value(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QSettings value(QString) expects one string key"));
            return success(toJson(settings->value(args.first().toString())));
        }
        if (auto *settings = qobject_cast<QSettings *>(object); settings != nullptr && signature == QStringLiteral("setValue(QString,QVariant)")) {
            if (args.size() != 2 || !args.first().isString()) return failure(QStringLiteral("QSettings setValue expects a string key and JSON value"));
            settings->setValue(args.first().toString(), args.at(1).toVariant());
            return success();
        }
        if (auto *settings = qobject_cast<QSettings *>(object); settings != nullptr && signature == QStringLiteral("contains(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QSettings contains(QString) expects one string key"));
            return success(QJsonValue(settings->contains(args.first().toString())));
        }
        if (auto *settings = qobject_cast<QSettings *>(object); settings != nullptr && signature == QStringLiteral("remove(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QSettings remove(QString) expects one string key"));
            settings->remove(args.first().toString());
            return success();
        }
        if (auto *settings = qobject_cast<QSettings *>(object); settings != nullptr && signature == QStringLiteral("allKeys()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QSettings allKeys() takes no arguments"));
            return success(QJsonValue::fromVariant(settings->allKeys()));
        }
        if (auto *settings = qobject_cast<QSettings *>(object); settings != nullptr && signature == QStringLiteral("sync()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QSettings sync() takes no arguments"));
            settings->sync();
            if (settings->status() != QSettings::NoError) return failure(QStringLiteral("QSettings could not synchronize its backing store"));
            return success();
        }
        if (auto *watcher = qobject_cast<QFileSystemWatcher *>(object); watcher != nullptr && signature == QStringLiteral("addPath(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QFileSystemWatcher addPath(QString) expects one path string"));
            if (!watcher->addPath(args.first().toString())) return failure(QStringLiteral("QFileSystemWatcher could not watch the path"));
            return success(QJsonValue(true));
        }
        if (auto *watcher = qobject_cast<QFileSystemWatcher *>(object); watcher != nullptr && signature == QStringLiteral("removePath(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QFileSystemWatcher removePath(QString) expects one path string"));
            if (!watcher->removePath(args.first().toString())) return failure(QStringLiteral("QFileSystemWatcher was not watching the path"));
            return success(QJsonValue(true));
        }
        if (auto *watcher = qobject_cast<QFileSystemWatcher *>(object); watcher != nullptr && signature == QStringLiteral("files()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QFileSystemWatcher files() takes no arguments"));
            return success(QJsonValue::fromVariant(watcher->files()));
        }
        if (auto *watcher = qobject_cast<QFileSystemWatcher *>(object); watcher != nullptr && signature == QStringLiteral("directories()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QFileSystemWatcher directories() takes no arguments"));
            return success(QJsonValue::fromVariant(watcher->directories()));
        }
        if (auto *process = qobject_cast<QProcess *>(object); process != nullptr && signature == QStringLiteral("readAllStandardOutput()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QProcess readAllStandardOutput() takes no arguments"));
            return success(toJson(QVariant::fromValue(process->readAllStandardOutput())));
        }
        if (auto *process = qobject_cast<QProcess *>(object); process != nullptr && signature == QStringLiteral("readAllStandardError()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QProcess readAllStandardError() takes no arguments"));
            return success(toJson(QVariant::fromValue(process->readAllStandardError())));
        }
        if (auto *process = qobject_cast<QProcess *>(object); process != nullptr && signature == QStringLiteral("exitCode()")) {
            if (!args.isEmpty()) return failure(QStringLiteral("QProcess exitCode() takes no arguments"));
            return success(QJsonValue(process->exitCode()));
        }
        if (auto *window = qobject_cast<QMainWindow *>(object); window != nullptr && signature == QStringLiteral("setMenuBar(QMenuBar*)")) {
            if (args.size() != 1) return failure(QStringLiteral("setMenuBar expects one menu bar handle"));
            auto *menuBar = qobject_cast<QMenuBar *>(objectArgument(args, 0, &error));
            if (menuBar == nullptr) return failure(error.isEmpty() ? QStringLiteral("setMenuBar expects a QMenuBar handle") : error);
            window->setMenuBar(menuBar);
            return success();
        }
        if (auto *window = qobject_cast<QMainWindow *>(object); window != nullptr && signature == QStringLiteral("addToolBar(QToolBar*)")) {
            if (args.size() != 1) return failure(QStringLiteral("addToolBar expects one toolbar handle"));
            auto *toolbar = qobject_cast<QToolBar *>(objectArgument(args, 0, &error));
            if (toolbar == nullptr) return failure(error.isEmpty() ? QStringLiteral("addToolBar expects a QToolBar handle") : error);
            window->addToolBar(toolbar);
            return success();
        }
        if (auto *window = qobject_cast<QMainWindow *>(object); window != nullptr && signature == QStringLiteral("addDockWidget(Qt::DockWidgetArea,QDockWidget*)")) {
            if (args.size() != 2) return failure(QStringLiteral("addDockWidget expects a dock area and dock widget handle"));
            int area = 0;
            auto *dock = qobject_cast<QDockWidget *>(objectArgument(args, 1, &error));
            if (dock == nullptr) return failure(error.isEmpty() ? QStringLiteral("addDockWidget expects a QDockWidget handle") : error);
            if (!integerArgument(0, &area) || (area != Qt::LeftDockWidgetArea && area != Qt::RightDockWidgetArea &&
                area != Qt::TopDockWidgetArea && area != Qt::BottomDockWidgetArea)) {
                return failure(QStringLiteral("dock area must be a Qt::DockWidgetArea integer value"));
            }
            window->addDockWidget(static_cast<Qt::DockWidgetArea>(area), dock);
            return success();
        }
        if (auto *widget = qobject_cast<QWidget *>(object); widget != nullptr && signature == QStringLiteral("setLayout(QLayout*)")) {
            if (args.size() != 1) return failure(QStringLiteral("setLayout expects one layout handle"));
            auto *layout = qobject_cast<QLayout *>(objectArgument(args, 0, &error));
            if (layout == nullptr) return failure(error.isEmpty() ? QStringLiteral("setLayout expects a QLayout handle") : error);
            if (widget->layout() == layout) return success();
            if (widget->layout() != nullptr && widget->layout() != layout) {
                return failure(QStringLiteral("QWidget already has a layout"));
            }
            widget->setLayout(layout);
            return success();
        }
        if (auto *layout = qobject_cast<QLayout *>(object); layout != nullptr && signature == QStringLiteral("setSpacing(int)")) {
            int spacing = 0;
            if (args.size() != 1 || !integerArgument(0, &spacing)) return failure(QStringLiteral("setSpacing expects one 32-bit integer"));
            layout->setSpacing(spacing);
            return success();
        }
        if (auto *layout = qobject_cast<QLayout *>(object); layout != nullptr && signature == QStringLiteral("setContentsMargins(int,int,int,int)")) {
            int left = 0, top = 0, right = 0, bottom = 0;
            if (args.size() != 4 || !integerArgument(0, &left) || !integerArgument(1, &top) ||
                !integerArgument(2, &right) || !integerArgument(3, &bottom)) {
                return failure(QStringLiteral("setContentsMargins expects four 32-bit integers"));
            }
            layout->setContentsMargins(left, top, right, bottom);
            return success();
        }
        if (auto *window = qobject_cast<QMainWindow *>(object); window != nullptr && signature == QStringLiteral("setCentralWidget(QWidget*)")) {
            if (args.size() != 1 || !args.first().isObject()) return failure(QStringLiteral("setCentralWidget requires one Qt object handle"));
            qint64 childId = 0;
            if (!parseObjectId(args.first().toObject().value(QStringLiteral("object_id")), &childId, &error)) return failure(error);
            auto *child = qobject_cast<QWidget *>(objectForId(childId));
            if (child == nullptr) return failure(QStringLiteral("setCentralWidget expects a QWidget handle"));
            window->setCentralWidget(child);
            return success();
        }
        if (auto *layout = qobject_cast<QLayout *>(object); layout != nullptr && signature == QStringLiteral("addWidget(QWidget*)")) {
            if (args.size() != 1 || !args.first().isObject()) return failure(QStringLiteral("addWidget requires one Qt object handle"));
            qint64 childId = 0;
            if (!parseObjectId(args.first().toObject().value(QStringLiteral("object_id")), &childId, &error)) return failure(error);
            auto *child = qobject_cast<QWidget *>(objectForId(childId));
            if (child == nullptr) return failure(QStringLiteral("addWidget expects a QWidget handle"));
            layout->addWidget(child);
            return success();
        }
        if (auto *box = qobject_cast<QBoxLayout *>(object); box != nullptr && signature == QStringLiteral("addLayout(QLayout*)")) {
            if (args.size() != 1 || !args.first().isObject()) return failure(QStringLiteral("addLayout requires one Qt object handle"));
            qint64 childId = 0;
            if (!parseObjectId(args.first().toObject().value(QStringLiteral("object_id")), &childId, &error)) return failure(error);
            auto *child = qobject_cast<QLayout *>(objectForId(childId));
            if (child == nullptr) return failure(QStringLiteral("addLayout expects a QLayout handle"));
            box->addLayout(child);
            return success();
        }
        if (auto *combo = qobject_cast<QComboBox *>(object); combo != nullptr && signature == QStringLiteral("addItem(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("addItem(QString) requires one string"));
            combo->addItem(args.first().toString());
            return success();
        }
        if (auto *list = qobject_cast<QListWidget *>(object); list != nullptr && signature == QStringLiteral("addItem(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("addItem(QString) requires one string"));
            list->addItem(args.first().toString());
            return success(QJsonValue(list->count() - 1));
        }
        if (auto *list = qobject_cast<QListWidget *>(object); list != nullptr && signature == QStringLiteral("itemText(int)")) {
            int row = -1;
            if (args.size() != 1 || !integerArgument(0, &row)) return failure(QStringLiteral("itemText(int) requires one 32-bit row index"));
            if (row < 0 || row >= list->count()) return failure(QStringLiteral("QListWidget row is outside its current item range"));
            return success(QJsonValue(list->item(row)->text()));
        }
        if (auto *table = qobject_cast<QTableWidget *>(object); table != nullptr && signature == QStringLiteral("setCellText(int,int,QString)")) {
            int row = -1, column = -1;
            if (args.size() != 3 || !integerArgument(0, &row) || !integerArgument(1, &column) || !args.at(2).isString()) {
                return failure(QStringLiteral("setCellText(int,int,QString) requires two 32-bit indices and a string"));
            }
            if (row < 0 || row >= table->rowCount() || column < 0 || column >= table->columnCount()) {
                return failure(QStringLiteral("QTableWidget cell is outside its current row or column range"));
            }
            QTableWidgetItem *item = table->item(row, column);
            if (item == nullptr) table->setItem(row, column, new QTableWidgetItem(args.at(2).toString()));
            else item->setText(args.at(2).toString());
            return success();
        }
        if (auto *table = qobject_cast<QTableWidget *>(object); table != nullptr && signature == QStringLiteral("cellText(int,int)")) {
            int row = -1, column = -1;
            if (args.size() != 2 || !integerArgument(0, &row) || !integerArgument(1, &column)) {
                return failure(QStringLiteral("cellText(int,int) requires two 32-bit indices"));
            }
            if (row < 0 || row >= table->rowCount() || column < 0 || column >= table->columnCount()) {
                return failure(QStringLiteral("QTableWidget cell is outside its current row or column range"));
            }
            const QTableWidgetItem *item = table->item(row, column);
            return success(item == nullptr ? QJsonValue(QString()) : QJsonValue(item->text()));
        }
        if (auto *tree = qobject_cast<QTreeWidget *>(object); tree != nullptr && signature == QStringLiteral("addTopLevelItem(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("addTopLevelItem(QString) requires one string"));
            auto *item = new QTreeWidgetItem(QStringList{args.first().toString()});
            tree->addTopLevelItem(item);
            return success(QJsonValue(tree->indexOfTopLevelItem(item)));
        }
        if (auto *tree = qobject_cast<QTreeWidget *>(object); tree != nullptr && signature == QStringLiteral("topLevelItemText(int)")) {
            int row = -1;
            if (args.size() != 1 || !integerArgument(0, &row)) return failure(QStringLiteral("topLevelItemText(int) requires one 32-bit row index"));
            if (row < 0 || row >= tree->topLevelItemCount()) return failure(QStringLiteral("QTreeWidget row is outside its current top-level item range"));
            return success(QJsonValue(tree->topLevelItem(row)->text(0)));
        }
        if (auto *tabs = qobject_cast<QTabWidget *>(object); tabs != nullptr && signature == QStringLiteral("addTab(QWidget*,QString)")) {
            if (args.size() != 2 || !args.at(0).isObject() || !args.at(1).isString()) return failure(QStringLiteral("addTab requires a Qt widget handle and a string"));
            qint64 childId = 0;
            if (!parseObjectId(args.at(0).toObject().value(QStringLiteral("object_id")), &childId, &error)) return failure(error);
            auto *child = qobject_cast<QWidget *>(objectForId(childId));
            if (child == nullptr) return failure(QStringLiteral("addTab expects a QWidget handle"));
            return success(QJsonValue(tabs->addTab(child, args.at(1).toString())));
        }
        if (auto *grid = qobject_cast<QGridLayout *>(object); grid != nullptr &&
            (signature == QStringLiteral("addWidget(QWidget*,int,int)") || signature == QStringLiteral("addWidget(QWidget*,int,int,int,int)"))) {
            const qsizetype expected = signature == QStringLiteral("addWidget(QWidget*,int,int)") ? 3 : 5;
            if (args.size() != expected) return failure(QStringLiteral("QGridLayout addWidget received the wrong argument count"));
            auto *child = qobject_cast<QWidget *>(objectArgument(args, 0, &error));
            int row = 0, column = 0, rowSpan = 1, columnSpan = 1;
            if (child == nullptr) return failure(error.isEmpty() ? QStringLiteral("QGridLayout expects a QWidget handle") : error);
            if (!integerArgument(1, &row) || !integerArgument(2, &column) ||
                (expected == 5 && (!integerArgument(3, &rowSpan) || !integerArgument(4, &columnSpan)))) {
                return failure(QStringLiteral("QGridLayout indices and spans must be 32-bit integers"));
            }
            if (expected == 3) grid->addWidget(child, row, column);
            else grid->addWidget(child, row, column, rowSpan, columnSpan);
            return success();
        }
        if (auto *form = qobject_cast<QFormLayout *>(object); form != nullptr && signature == QStringLiteral("addRow(QString,QWidget*)")) {
            if (args.size() != 2 || !args.first().isString()) return failure(QStringLiteral("QFormLayout addRow(QString,QWidget*) expects a string and a widget handle"));
            auto *field = qobject_cast<QWidget *>(objectArgument(args, 1, &error));
            if (field == nullptr) return failure(error.isEmpty() ? QStringLiteral("QFormLayout expects a QWidget handle") : error);
            form->addRow(args.first().toString(), field);
            return success();
        }
        if (auto *form = qobject_cast<QFormLayout *>(object); form != nullptr && signature == QStringLiteral("addRow(QWidget*,QWidget*)")) {
            if (args.size() != 2) return failure(QStringLiteral("QFormLayout addRow(QWidget*,QWidget*) expects two widget handles"));
            auto *label = qobject_cast<QWidget *>(objectArgument(args, 0, &error));
            if (label == nullptr) return failure(error.isEmpty() ? QStringLiteral("QFormLayout expects QWidget handles") : error);
            auto *field = qobject_cast<QWidget *>(objectArgument(args, 1, &error));
            if (field == nullptr) return failure(error.isEmpty() ? QStringLiteral("QFormLayout expects QWidget handles") : error);
            form->addRow(label, field);
            return success();
        }
        if (auto *stack = qobject_cast<QStackedWidget *>(object); stack != nullptr && signature == QStringLiteral("addWidget(QWidget*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QStackedWidget addWidget expects one widget handle"));
            auto *child = qobject_cast<QWidget *>(objectArgument(args, 0, &error));
            if (child == nullptr) return failure(error.isEmpty() ? QStringLiteral("QStackedWidget expects a QWidget handle") : error);
            return success(QJsonValue(stack->addWidget(child)));
        }
        if (auto *splitter = qobject_cast<QSplitter *>(object); splitter != nullptr && signature == QStringLiteral("addWidget(QWidget*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QSplitter addWidget expects one widget handle"));
            auto *child = qobject_cast<QWidget *>(objectArgument(args, 0, &error));
            if (child == nullptr) return failure(error.isEmpty() ? QStringLiteral("QSplitter expects a QWidget handle") : error);
            splitter->addWidget(child);
            return success();
        }
        if (auto *scrollArea = qobject_cast<QScrollArea *>(object); scrollArea != nullptr && signature == QStringLiteral("setWidget(QWidget*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QScrollArea setWidget expects one widget handle"));
            auto *child = qobject_cast<QWidget *>(objectArgument(args, 0, &error));
            if (child == nullptr) return failure(error.isEmpty() ? QStringLiteral("QScrollArea expects a QWidget handle") : error);
            scrollArea->setWidget(child);
            return success();
        }
        if (auto *scene = qobject_cast<QGraphicsScene *>(object); scene != nullptr && signature == QStringLiteral("addWidget(QWidget*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QGraphicsScene addWidget expects one widget handle"));
            auto *child = qobject_cast<QWidget *>(objectArgument(args, 0, &error));
            if (child == nullptr) return failure(error.isEmpty() ? QStringLiteral("QGraphicsScene expects a QWidget handle") : error);
            return success(objectHandleValue(scene->addWidget(child)));
        }
        if (auto *view = qobject_cast<QGraphicsView *>(object); view != nullptr && signature == QStringLiteral("setScene(QGraphicsScene*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QGraphicsView setScene expects one scene handle"));
            auto *scene = qobject_cast<QGraphicsScene *>(objectArgument(args, 0, &error));
            if (scene == nullptr) return failure(error.isEmpty() ? QStringLiteral("QGraphicsView expects a QGraphicsScene handle") : error);
            view->setScene(scene);
            return success();
        }
        if (auto *edit = qobject_cast<QTextEdit *>(object); edit != nullptr && signature == QStringLiteral("setDocument(QTextDocument*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QTextEdit setDocument expects one document handle"));
            auto *document = qobject_cast<QTextDocument *>(objectArgument(args, 0, &error));
            if (document == nullptr) return failure(error.isEmpty() ? QStringLiteral("QTextEdit expects a QTextDocument handle") : error);
            edit->setDocument(document);
            return success();
        }
        if (auto *view = qobject_cast<QUndoView *>(object); view != nullptr && signature == QStringLiteral("setStack(QUndoStack*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QUndoView setStack expects one undo stack handle"));
            auto *stack = qobject_cast<QUndoStack *>(objectArgument(args, 0, &error));
            if (stack == nullptr) return failure(error.isEmpty() ? QStringLiteral("QUndoView expects a QUndoStack handle") : error);
            view->setStack(stack);
            return success();
        }
        if (auto *view = qobject_cast<QUndoView *>(object); view != nullptr && signature == QStringLiteral("setGroup(QUndoGroup*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QUndoView setGroup expects one undo group handle"));
            auto *group = qobject_cast<QUndoGroup *>(objectArgument(args, 0, &error));
            if (group == nullptr) return failure(error.isEmpty() ? QStringLiteral("QUndoView expects a QUndoGroup handle") : error);
            view->setGroup(group);
            return success();
        }
        if (auto *wizard = qobject_cast<QWizard *>(object); wizard != nullptr && signature == QStringLiteral("addPage(QWizardPage*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QWizard addPage expects one page handle"));
            auto *page = qobject_cast<QWizardPage *>(objectArgument(args, 0, &error));
            if (page == nullptr) return failure(error.isEmpty() ? QStringLiteral("QWizard expects a QWizardPage handle") : error);
            return success(QJsonValue(wizard->addPage(page)));
        }
        if (auto *group = qobject_cast<QButtonGroup *>(object); group != nullptr &&
            (signature == QStringLiteral("addButton(QAbstractButton*)") || signature == QStringLiteral("addButton(QAbstractButton*,int)"))) {
            if (args.size() != (signature == QStringLiteral("addButton(QAbstractButton*)") ? 1 : 2)) {
                return failure(QStringLiteral("QButtonGroup addButton received the wrong argument count"));
            }
            auto *button = qobject_cast<QAbstractButton *>(objectArgument(args, 0, &error));
            int buttonId = -1;
            if (button == nullptr) return failure(error.isEmpty() ? QStringLiteral("QButtonGroup expects a QAbstractButton handle") : error);
            if (args.size() == 2 && !integerArgument(1, &buttonId)) return failure(QStringLiteral("QButtonGroup id must be a 32-bit integer"));
            group->addButton(button, buttonId);
            return success();
        }
        if (auto *tabBar = qobject_cast<QTabBar *>(object); tabBar != nullptr && signature == QStringLiteral("addTab(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QTabBar addTab(QString) expects one string"));
            return success(QJsonValue(tabBar->addTab(args.first().toString())));
        }
        if (auto *menu = qobject_cast<QMenu *>(object); menu != nullptr && signature == QStringLiteral("addAction(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QMenu addAction(QString) expects one string"));
            QAction *action = menu->addAction(args.first().toString());
            return success(objectHandleValue(action));
        }
        if (auto *menu = qobject_cast<QMenu *>(object); menu != nullptr && signature == QStringLiteral("addAction(QAction*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QMenu addAction(QAction*) expects one action handle"));
            auto *action = qobject_cast<QAction *>(objectArgument(args, 0, &error));
            if (action == nullptr) return failure(error.isEmpty() ? QStringLiteral("QMenu expects a QAction handle") : error);
            menu->addAction(action);
            return success();
        }
        if (auto *menuBar = qobject_cast<QMenuBar *>(object); menuBar != nullptr && signature == QStringLiteral("addMenu(QMenu*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QMenuBar addMenu(QMenu*) expects one menu handle"));
            auto *menu = qobject_cast<QMenu *>(objectArgument(args, 0, &error));
            if (menu == nullptr) return failure(error.isEmpty() ? QStringLiteral("QMenuBar expects a QMenu handle") : error);
            QAction *action = menuBar->addMenu(menu);
            return success(objectHandleValue(action));
        }
        if (auto *menuBar = qobject_cast<QMenuBar *>(object); menuBar != nullptr && signature == QStringLiteral("addMenu(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QMenuBar addMenu(QString) expects one string"));
            QMenu *menu = menuBar->addMenu(args.first().toString());
            return success(objectHandleValue(menu));
        }
        if (auto *toolbar = qobject_cast<QToolBar *>(object); toolbar != nullptr && signature == QStringLiteral("addAction(QString)")) {
            if (args.size() != 1 || !args.first().isString()) return failure(QStringLiteral("QToolBar addAction(QString) expects one string"));
            QAction *action = toolbar->addAction(args.first().toString());
            return success(objectHandleValue(action));
        }
        if (auto *toolbar = qobject_cast<QToolBar *>(object); toolbar != nullptr && signature == QStringLiteral("addAction(QAction*)")) {
            if (args.size() != 1) return failure(QStringLiteral("QToolBar addAction(QAction*) expects one action handle"));
            auto *action = qobject_cast<QAction *>(objectArgument(args, 0, &error));
            if (action == nullptr) return failure(error.isEmpty() ? QStringLiteral("QToolBar expects a QAction handle") : error);
            toolbar->addAction(action);
            return success();
        }
        QJsonValue directResult;
        QString directError;
        if (kry_qt6_direct::invoke(object, QString(), signature, args, &directResult, &directError)) {
            if (!directError.isEmpty()) return failure(directError);
            return success(directResult);
        }
        QJsonValue result;
        if (!invokeMethod(object->metaObject(), object, nullptr, signature, args, &result, &error)) return failure(error);
        return success(result);
    }

    if (operation == QStringLiteral("connect")) {
        QString rawSignature = request.value(QStringLiteral("signal")).toString();
        if (rawSignature.isEmpty() || hasNul(rawSignature)) return failure(QStringLiteral("connect requires a signal signature without NUL characters"));
        QByteArray signature = QMetaObject::normalizedSignature(rawSignature.toUtf8().constData());
        const int signalIndex = object->metaObject()->indexOfSignal(signature.constData());
        if (signalIndex < 0) return failure(QStringLiteral("signal %1 does not exist on %2").arg(rawSignature, QString::fromLatin1(object->metaObject()->className())));
        auto subscription = std::make_unique<Subscription>();
        subscription->objectId = id;
        subscription->signature = signature;
        subscription->spy = std::make_unique<QSignalSpy>(object, object->metaObject()->method(signalIndex));
        if (!subscription->spy->isValid()) return failure(QStringLiteral("Qt cannot capture signal %1 because one or more argument types are not registered").arg(rawSignature));
        subscriptions[id].push_back(std::move(subscription));
        return success();
    }

    if (operation == QStringLiteral("delete")) {
        std::vector<qint64> invalidated;
        for (const auto &entry : objects) {
            QObject *candidate = entry.second.data();
            if (candidate == nullptr || candidate == object || isDescendantOf(candidate, object)) {
                invalidated.push_back(entry.first);
            }
        }
        for (qint64 invalidatedId : invalidated) {
            subscriptions.erase(invalidatedId);
            objects.erase(invalidatedId);
        }
        delete object;
        return success();
    }

    return failure(QStringLiteral("unsupported Qt operation: %1").arg(operation));
}

int32_t writeResponse(const QJsonObject &object, char *output, int32_t capacity) {
    QByteArray responseJson = QJsonDocument(object).toJson(QJsonDocument::Compact);
    constexpr qsizetype maximumJsonBytes = (kMaxResponseBytes / 4) * 3 - 3;
    if (responseJson.size() > maximumJsonBytes) {
        responseJson = QJsonDocument(failure(QStringLiteral("Qt response exceeds the 16 MiB wire limit")))
                           .toJson(QJsonDocument::Compact);
    }
    const QByteArray encoded = responseJson.toBase64(QByteArray::Base64Encoding);
    const int32_t length = static_cast<int32_t>(encoded.size());
    if (output == nullptr || capacity < 0 || capacity > kMaxResponseBytes) return 0;
    if (encoded.size() > capacity) return length;
    if (length > 0) std::memcpy(output, encoded.constData(), static_cast<size_t>(length));
    return length;
}

} // namespace

namespace kry_qt6_direct {

QVariant fromJson(const QJsonValue &value, const QMetaType &target, QString *error) {
    return ::fromJson(value, target, error);
}

QJsonValue toJson(const QVariant &value) {
    return ::toJson(value);
}

QJsonValue objectHandle(QObject *object) {
    if (object == nullptr) return QJsonValue(QJsonValue::Null);
    return ::objectHandleValue(object);
}

QObject *adopt(QObject *object, QObject *parent, QString *error) {
    if (object == nullptr || parent == nullptr) return object;
    if (auto *widget = qobject_cast<QWidget *>(object)) {
        if (object->parent() == parent) return object;
        auto *widgetParent = qobject_cast<QWidget *>(parent);
        if (widgetParent == nullptr) {
            *error = QStringLiteral("widget constructors require a QWidget parent");
            return nullptr;
        }
        widget->setParent(widgetParent);
    } else if (auto *window = qobject_cast<QWindow *>(object)) {
        if (object->parent() == parent) return object;
        auto *windowParent = qobject_cast<QWindow *>(parent);
        if (windowParent == nullptr) {
            *error = QStringLiteral("window constructors require a QWindow parent");
            return nullptr;
        }
        window->setParent(windowParent);
    } else if (auto *layout = qobject_cast<QLayout *>(object)) {
        auto *widgetParent = qobject_cast<QWidget *>(parent);
        if (widgetParent == nullptr) {
            *error = QStringLiteral("layout constructors require a QWidget parent");
            return nullptr;
        }
        if (widgetParent->layout() == layout) return object;
        if (widgetParent->layout() != nullptr) {
            *error = QStringLiteral("QWidget already has a layout");
            return nullptr;
        }
        widgetParent->setLayout(layout);
    } else if (auto *node = qobject_cast<Qt3DCore::QNode *>(object)) {
        if (object->parent() == parent) return object;
        auto *nodeParent = qobject_cast<Qt3DCore::QNode *>(parent);
        if (nodeParent == nullptr) {
            *error = QStringLiteral("Qt 3D constructors require a Qt3DCore.QNode parent");
            return nullptr;
        }
        node->setParent(nodeParent);
    } else {
        if (object->parent() == parent) return object;
        object->setParent(parent);
    }
    return object;
}

} // namespace kry_qt6_direct

extern "C" KRY_QT6_EXPORT int32_t kry_qt6_abi_version(void) {
    return 1;
}

extern "C" KRY_QT6_EXPORT int32_t kry_qt6_request(const char *request, int32_t requestLength,
                                                    char *response, int32_t capacity) {
    if (request == nullptr || requestLength < 2 || requestLength > kMaxRequestBytes ||
        response == nullptr || capacity < 1 || capacity > kMaxResponseBytes) {
        return 0;
    }
    QJsonParseError parseError;
    const QJsonDocument document = QJsonDocument::fromJson(QByteArray(request, requestLength), &parseError);
    if (parseError.error != QJsonParseError::NoError || !document.isObject()) {
        std::lock_guard<std::mutex> lock(stateMutex);
        return writeResponse(failure(QStringLiteral("request must be a JSON object: %1").arg(parseError.errorString())), response, capacity);
    }
    std::lock_guard<std::mutex> lock(stateMutex);
    try {
        return writeResponse(handleRequest(document.object()), response, capacity);
    } catch (const std::exception &exception) {
        return writeResponse(failure(QString::fromUtf8(exception.what())), response, capacity);
    } catch (...) {
        return writeResponse(failure(QStringLiteral("unknown exception inside Qt bridge")), response, capacity);
    }
}
