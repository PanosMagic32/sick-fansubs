import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import { Document, Schema as MongooseSchema, Types } from 'mongoose';

export type UserDocument = User & Document;

@Schema({ timestamps: true })
export class User {
  @Prop({ required: true, unique: true })
  username!: string;

  @Prop({ required: true })
  password!: string;

  @Prop({ required: true, unique: true })
  email!: string;

  @Prop()
  avatar!: string;

  @Prop({ default: false })
  isAdmin!: boolean;

  @Prop({ type: [MongooseSchema.Types.ObjectId], ref: 'BlogPost', default: [] })
  favoriteBlogPostIds!: Types.ObjectId[];

  @Prop({ type: [MongooseSchema.Types.ObjectId], ref: 'BlogPost', default: [] })
  createdBlogPostIds!: Types.ObjectId[];
}

export const UserSchema = SchemaFactory.createForClass(User);

UserSchema.set('toJSON', {
  transform: (_doc, ret) => {
    delete (ret as unknown as Record<string, unknown>)['password'];
    return ret;
  },
});
