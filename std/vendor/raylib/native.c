#include "raylib.h"

Color SealRaylibColor(
    unsigned char red,
    unsigned char green,
    unsigned char blue,
    unsigned char alpha
) {
    Color color = {
        .r = red,
        .g = green,
        .b = blue,
        .a = alpha,
    };

    return color;
}

unsigned char SealRaylibColorRed(
    Color color
) {
    return color.r;
}

unsigned char SealRaylibColorGreen(
    Color color
) {
    return color.g;
}

unsigned char SealRaylibColorBlue(
    Color color
) {
    return color.b;
}

unsigned char SealRaylibColorAlpha(
    Color color
) {
    return color.a;
}

Vector2 SealRaylibVector2(
    float x,
    float y
) {
    Vector2 vector = {
        .x = x,
        .y = y,
    };

    return vector;
}

float SealRaylibVector2X(
    Vector2 vector
) {
    return vector.x;
}

float SealRaylibVector2Y(
    Vector2 vector
) {
    return vector.y;
}

Rectangle SealRaylibRectangle(
    float x,
    float y,
    float width,
    float height
) {
    Rectangle rectangle = {
        .x = x,
        .y = y,
        .width = width,
        .height = height,
    };

    return rectangle;
}

float SealRaylibRectangleX(
    Rectangle rectangle
) {
    return rectangle.x;
}

float SealRaylibRectangleY(
    Rectangle rectangle
) {
    return rectangle.y;
}

float SealRaylibRectangleWidth(
    Rectangle rectangle
) {
    return rectangle.width;
}

float SealRaylibRectangleHeight(
    Rectangle rectangle
) {
    return rectangle.height;
}
