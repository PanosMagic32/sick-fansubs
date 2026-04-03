import { Prop, Schema, SchemaFactory } from '@nestjs/mongoose';
import type { UserRole, UserStatus } from '@shared/types';
import { Document, Schema as MongooseSchema, Types } from 'mongoose';

export type UserDocument = User & Document;

const USER_ROLES: UserRole[] = ['super-admin', 'admin', 'moderator', 'user'];
const USER_STATUSES: UserStatus[] = ['active', 'suspended'];

@Schema({ timestamps: true })
export class User {
  @Prop({ type: String, required: true, unique: true })
  username!: string;

  @Prop({ type: String, required: true })
  password!: string;

  @Prop({ type: String, required: true, unique: true })
  email!: string;

  @Prop({ type: String })
  avatar!: string;

  @Prop({ type: String, enum: USER_ROLES, default: 'user' })
  role!: UserRole;

  @Prop({ type: String, enum: USER_STATUSES, default: 'active' })
  status!: UserStatus;

  @Prop({ type: [MongooseSchema.Types.ObjectId], ref: 'BlogPost', default: [] })
  favoriteBlogPostIds!: Types.ObjectId[];

  @Prop({ type: [MongooseSchema.Types.ObjectId], ref: 'BlogPost', default: [] })
  createdBlogPostIds!: Types.ObjectId[];

  @Prop({ type: String })
  refreshTokenHash?: string;

  @Prop({ type: String })
  refreshTokenJti?: string;

  @Prop({ type: Date })
  refreshTokenExpiresAt?: Date;
}

export const UserSchema = SchemaFactory.createForClass(User);

UserSchema.set('toJSON', {
  transform: (_doc, ret) => {
    delete (ret as unknown as Record<string, unknown>)['password'];
    return ret;
  },
});
