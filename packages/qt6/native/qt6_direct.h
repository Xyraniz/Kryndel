#ifndef KRY_QT6_DIRECT_H
#define KRY_QT6_DIRECT_H

#include <QtCore/QJsonArray>
#include <QtCore/QJsonValue>
#include <QtCore/QMetaType>
#include <QtCore/QObject>
#include <QtCore/QString>
#include <QtCore/QVariant>
#include <type_traits>
#include <utility>
#include <QtCore/QByteArray>
#include <QtCore/QDate>
#include <QtCore/QDateTime>
#include <QtCore/QLocale>
#include <QtCore/QMimeDatabase>
#include <QtCore/QMimeType>
#include <QtCore/QModelIndex>
#include <QtCore/QPersistentModelIndex>
#include <QtCore/QJsonArray>
#include <QtCore/QJsonObject>
#include <QtCore/QJsonValue>
#include <QtCore/QLine>
#include <QtCore/QLineF>
#include <QtCore/QMargins>
#include <QtCore/QMarginsF>
#include <QtCore/QPoint>
#include <QtCore/QPointF>
#include <QtCore/QRect>
#include <QtCore/QRectF>
#include <QtCore/QRegularExpression>
#include <QtCore/QRegularExpressionMatch>
#include <QtCore/QRegularExpressionMatchIterator>
#include <QtCore/QSize>
#include <QtCore/QSizeF>
#include <QtCore/QStorageInfo>
#include <QtCore/QTime>
#include <QtCore/QTimeZone>
#include <QtCore/QUrl>
#include <QtCore/QUrlQuery>
#include <QtCore/QUuid>
#include <QtCore/QVersionNumber>
#include <QtGui/QColor>
#include <QtGui/QFont>
#include <QtGui/QIcon>
#include <QtGui/QImage>
#include <QtGui/QKeySequence>
#include <QtGui/QMatrix4x4>
#include <QtGui/QPainterPath>
#include <QtGui/QPolygon>
#include <QtGui/QPolygonF>
#include <QtGui/QPixmap>
#include <QtGui/QQuaternion>
#include <QtGui/QRegion>
#include <QtGui/QTextBlockFormat>
#include <QtGui/QTextCharFormat>
#include <QtGui/QTextCursor>
#include <QtGui/QTextFormat>
#include <QtGui/QTextFrameFormat>
#include <QtGui/QTextImageFormat>
#include <QtGui/QTextLength>
#include <QtGui/QTextListFormat>
#include <QtGui/QTextOption>
#include <QtGui/QTextTableFormat>
#include <QtGui/QTransform>
#include <QtGui/QVector2D>
#include <QtGui/QVector3D>
#include <QtGui/QVector4D>
#include <QtNetwork/QNetworkCookie>
#include <QtNetwork/QNetworkProxy>
#include <QtNetwork/QSslCertificate>
#include <QtNetwork/QSslConfiguration>
#include <QtPositioning/QGeoAddress>
#include <QtPositioning/QGeoCircle>
#include <QtPositioning/QGeoPath>
#include <QtPositioning/QGeoPolygon>
#include <QtPositioning/QGeoRectangle>

namespace kry_qt6_direct {

template <typename T>
QVariant storeValue(const T &value) {
    if constexpr (QMetaTypeId2<T>::Defined && std::is_copy_constructible_v<T>)
        return QVariant::fromValue<T>(value);
    else
        return {};
}

QVariant fromJson(const QJsonValue &value, const QMetaType &target, QString *error);
QJsonValue toJson(const QVariant &value);
QJsonValue objectHandle(QObject *object);
QObject *adopt(QObject *object, QObject *parent, QString *error);

bool invoke(QObject *object, const QString &className, const QString &signature, const QJsonArray &args,
            QJsonValue *output, QString *error);

} // namespace kry_qt6_direct

#endif
